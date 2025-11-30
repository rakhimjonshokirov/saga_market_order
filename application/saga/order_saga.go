package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"market_order/application/usecases"
	"market_order/domain/order"
	"market_order/domain/position"
	"market_order/infrastructure/idempotency"
	"market_order/infrastructure/messaging"
	"market_order/infrastructure/repository"
	pkguuid "market_order/pkg/uuid"
)

// OrderSaga orchestrates the order execution workflow
type OrderSaga struct {
	orderRepo       *repository.OrderRepository
	positionRepo    *repository.PositionRepository
	processedEvents *idempotency.ProcessedEventsRepository
	createOrderUC   *usecases.CreateOrderUseCase
	completeOrderUC *usecases.CompleteOrderAndUpdatePositionUseCase
	messageBus      *messaging.RabbitMQ
	priceService    PriceService
	tradeWorker     TradeWorker
}

// PriceService интерфейс для получения цен
type PriceService interface {
	GetMarketPrice(ctx context.Context, from, to string) (float64, error)
}

// TradeWorker интерфейс для исполнения swap
type TradeWorker interface {
	ExecuteSwap(ctx context.Context, req SwapRequest) (*SwapResponse, error)
}

type SwapRequest struct {
	IdempotencyKey string
	FromCurrency   string
	ToCurrency     string
	FromAmount     float64
	Slippage       float64
}

type SwapResponse struct {
	TransactionHash string
	ToAmount        float64
	ExecutedPrice   float64
	Fees            float64
	Slippage        float64
}

func NewOrderSaga(
	orderRepo *repository.OrderRepository,
	positionRepo *repository.PositionRepository,
	processedEvents *idempotency.ProcessedEventsRepository,
	createOrderUC *usecases.CreateOrderUseCase,
	completeOrderUC *usecases.CompleteOrderAndUpdatePositionUseCase,
	messageBus *messaging.RabbitMQ,
	priceService PriceService,
	tradeWorker TradeWorker,
) *OrderSaga {
	return &OrderSaga{
		orderRepo:       orderRepo,
		positionRepo:    positionRepo,
		processedEvents: processedEvents,
		createOrderUC:   createOrderUC,
		completeOrderUC: completeOrderUC,
		messageBus:      messageBus,
		priceService:    priceService,
		tradeWorker:     tradeWorker,
	}
}

// Start запускает Saga orchestrator (слушает события)
func (s *OrderSaga) Start(ctx context.Context) error {
	// Подписываемся на события
	if err := s.messageBus.Subscribe("OrderAccepted", s.handleOrderAccepted); err != nil {
		return err
	}

	log.Println("✅ Order Saga started, listening for events...")

	<-ctx.Done()
	return nil
}

// handleOrderAccepted - обработчик события OrderAccepted
func (s *OrderSaga) handleOrderAccepted(ctx context.Context, eventData []byte) error {
	log.Println("📨 Saga: Received OrderAccepted event")

	// === IDEMPOTENCY CHECK ===
	var evt order.OrderAccepted
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Проверяем, обработано ли событие (Idempodency)
	processed, err := s.processedEvents.IsProcessed(ctx, evt.EventID)
	if err != nil {
		log.Printf("⚠️  Failed to check idempotency: %v", err)
		return err
	}

	if processed {
		log.Printf("⏭️  Event %s already processed, skipping", evt.EventID)
		return nil
	}

	// === STEP 1: Get Market Price ===
	log.Printf("📊 Step 1: Getting market price for %s/%s", evt.FromCurrency, evt.ToCurrency)

	price, err := s.priceService.GetMarketPrice(ctx, evt.FromCurrency, evt.ToCurrency)
	if err != nil {
		log.Printf("❌ Failed to get price: %v", err)
		return s.compensateOrderFailed(ctx, evt.AggregateID, "price_unavailable")
	}

	toAmount := evt.FromAmount / price
	log.Printf("✅ Price quoted: 1 %s = %.2f %s, toAmount = %.8f",
		evt.ToCurrency, price, evt.FromCurrency, toAmount)

	// Update order with price
	o, err := s.orderRepo.Get(ctx, evt.AggregateID)
	if err != nil {
		return err
	}

	if err := o.QuotePrice(price, toAmount); err != nil {
		return err
	}

	if err := s.orderRepo.Save(ctx, o); err != nil {
		return err
	}

	// === STEP 2: Create Position ===
	log.Printf("📦 Step 2: Creating position for user %s", evt.UserID)

	positionID := pkguuid.New()

	p := position.NewPosition()
	if err := p.CreatePosition(positionID, evt.UserID); err != nil {
		return err
	}

	if err := s.positionRepo.Save(ctx, p); err != nil {
		return err
	}

	log.Printf("✅ Position created: %s", positionID)

	// === STEP 3: Execute Swap ===
	log.Printf("🔄 Step 3: Executing swap for order %s", evt.AggregateID)

	idempotencyKey := generateIdempotencyKey(evt.AggregateID)

	// Mark as executing
	o, _ = s.orderRepo.Get(ctx, evt.AggregateID)
	if err := o.StartSwapExecution(idempotencyKey); err != nil {
		return err
	}
	s.orderRepo.Save(ctx, o)

	swapReq := SwapRequest{
		IdempotencyKey: idempotencyKey,
		FromCurrency:   evt.FromCurrency,
		ToCurrency:     evt.ToCurrency,
		FromAmount:     evt.FromAmount,
		Slippage:       0.5, // 0.5%
	}

	swapResp, err := s.tradeWorker.ExecuteSwap(ctx, swapReq)
	if err != nil {
		log.Printf("❌ Swap execution failed: %v", err)
		return s.compensateSwapFailed(ctx, evt.AggregateID, positionID, err.Error())
	}

	log.Printf("✅ Swap executed: txHash=%s", swapResp.TransactionHash)

	// Record swap execution
	o, _ = s.orderRepo.Get(ctx, evt.AggregateID)
	o.RecordSwapExecution(
		swapResp.TransactionHash,
		evt.FromAmount,
		swapResp.ToAmount,
		swapResp.ExecutedPrice,
		swapResp.Fees,
		swapResp.Slippage,
	)
	s.orderRepo.Save(ctx, o)

	// === STEP 4: Complete Order and Update Position (ATOMIC) ===
	log.Printf("✅ Step 4: Completing order and updating position (atomic transaction)")

	err = s.completeOrderUC.Execute(ctx, evt.AggregateID, positionID, usecases.SwapResult{
		TransactionHash: swapResp.TransactionHash,
		FromAmount:      evt.FromAmount,
		ToAmount:        swapResp.ToAmount,
		ExecutedPrice:   swapResp.ExecutedPrice,
		Fees:            swapResp.Fees,
		Slippage:        swapResp.Slippage,
	})

	if err != nil {
		log.Printf("❌ Failed to complete order: %v", err)
		// КРИТИЧЕСКАЯ СИТУАЦИЯ: swap выполнен, но не можем обновить state
		// Нужен алерт для ручного разбора или механизм компенсации
		return err
	}

	log.Printf("🎉 ✅ Order %s completed successfully!", evt.AggregateID)

	// === Mark event as processed (IDEMPOTENCY) ===
	err = s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga")
	if err != nil {
		log.Printf("⚠️  Failed to mark event as processed: %v (saga completed but idempotency not recorded)", err)
		// Не возвращаем ошибку, т.к. saga успешно завершена
	}

	return nil
}

// === COMPENSATION FUNCTIONS ===

func (s *OrderSaga) compensateOrderFailed(ctx context.Context, orderID, reason string) error {
	log.Printf("🔙 COMPENSATION: Failing order %s, reason: %s", orderID, reason)

	o, err := s.orderRepo.Get(ctx, orderID)
	if err != nil {
		return err
	}

	if err := o.FailOrder(reason); err != nil {
		return err
	}

	return s.orderRepo.Save(ctx, o)
}

func (s *OrderSaga) compensateSwapFailed(ctx context.Context, orderID, positionID, reason string) error {
	log.Printf("🔙 COMPENSATION: Swap failed for order %s", orderID)

	// Fail order
	if err := s.compensateOrderFailed(ctx, orderID, reason); err != nil {
		return err
	}

	// Close position
	p, err := s.positionRepo.Get(ctx, positionID)
	if err != nil {
		return err
	}

	if err := p.ClosePosition("order_failed"); err != nil {
		return err
	}

	return s.positionRepo.Save(ctx, p)
}

// === HELPERS ===

func generateIdempotencyKey(orderID string) string {
	return fmt.Sprintf("swap-%s", orderID)
}
