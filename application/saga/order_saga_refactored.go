package saga

import (
	"context"
	"log"

	"market_order/application/aggregates"
	"market_order/application/usecases"
	"market_order/infrastructure/idempotency"
	"market_order/infrastructure/messaging"
)

// OrderSagaRefactored orchestrates order execution with granular steps
//
// Architecture:
// - Each saga step is in a separate file (accept.go, price.go, swap.go, complete.go)
// - Each step listens to a specific event and publishes the next event
// - Steps are independent and can be scaled separately
// - Metadata is used to pass context (position_id) between steps
// - Uses EventStore as source of truth (NOT repositories!)
//
// Flow:
// OrderAccepted → [accept.go] → PriceQuoted
//
//	→ [price.go] → PositionCreatedForOrder
//	→ [swap.go] → SwapExecuted
//	→ [complete.go] → PositionLinkedToOrder
type OrderSagaRefactored struct {
	aggregateStore  *aggregates.AggregateStore // ✅ Source of truth
	processedEvents *idempotency.ProcessedEventsRepository
	completeOrderUC *usecases.CompleteOrderAndUpdatePositionUseCase
	messageBus      *messaging.RabbitMQ
	priceService    PriceService
	tradeWorker     TradeWorker
}

func NewOrderSagaRefactored(
	aggregateStore *aggregates.AggregateStore,
	processedEvents *idempotency.ProcessedEventsRepository,
	completeOrderUC *usecases.CompleteOrderAndUpdatePositionUseCase,
	messageBus *messaging.RabbitMQ,
	priceService PriceService,
	tradeWorker TradeWorker,
) *OrderSagaRefactored {
	return &OrderSagaRefactored{
		aggregateStore:  aggregateStore,
		processedEvents: processedEvents,
		completeOrderUC: completeOrderUC,
		messageBus:      messageBus,
		priceService:    priceService,
		tradeWorker:     tradeWorker,
	}
}

// Start запускает Saga orchestrator (слушает события)
//
// Subscribes to 4 events (one per step):
// 1. OrderAccepted      → handled in accept.go
// 2. PriceQuoted        → handled in price.go
// 3. PositionCreatedForOrder → handled in swap.go
// 4. SwapExecuted       → handled in complete.go
func (s *OrderSagaRefactored) Start(ctx context.Context) error {
	// STEP 1: Price quotation
	if err := s.messageBus.Subscribe("OrderAccepted", s.handleOrderAccepted); err != nil {
		return err
	}

	// STEP 2: Position creation
	if err := s.messageBus.Subscribe("PriceQuoted", s.handlePriceQuoted); err != nil {
		return err
	}

	// STEP 3: Swap execution
	if err := s.messageBus.Subscribe("PositionCreatedForOrder", s.handlePositionCreated); err != nil {
		return err
	}

	// STEP 4: Order completion
	if err := s.messageBus.Subscribe("SwapExecuted", s.handleSwapExecuted); err != nil {
		return err
	}

	log.Println("✅ Order Saga (Refactored) started with granular steps...")

	<-ctx.Done()
	return nil
}

// ===============================================
// COMPENSATION FUNCTIONS
// ===============================================

// compensateOrderFailed marks order as failed
// Used when early steps fail (price unavailable, validation errors)
func (s *OrderSagaRefactored) compensateOrderFailed(ctx context.Context, orderID, reason string) error {
	log.Printf("🔙 COMPENSATION: Failing order %s, reason: %s", orderID, reason)

	// Load aggregate from EventStore (source of truth)
	o, err := s.aggregateStore.LoadOrderAggregate(ctx, orderID)
	if err != nil {
		return err
	}

	// Generate FailOrder event
	if err := o.FailOrder(reason); err != nil {
		return err
	}

	// Save events to EventStore
	return s.aggregateStore.SaveOrderAggregate(ctx, o)
}

// compensateSwapFailed rolls back order and position when swap fails
// Used when swap execution fails (blockchain error, insufficient liquidity, etc.)
func (s *OrderSagaRefactored) compensateSwapFailed(ctx context.Context, orderID, positionID, reason string) error {
	log.Printf("🔙 COMPENSATION: Swap failed for order %s", orderID)

	// Fail order
	if err := s.compensateOrderFailed(ctx, orderID, reason); err != nil {
		return err
	}

	// Load position from EventStore
	p, err := s.aggregateStore.LoadPositionAggregate(ctx, positionID)
	if err != nil {
		return err
	}

	// Generate ClosePosition event
	if err := p.ClosePosition("order_failed"); err != nil {
		return err
	}

	// Save events to EventStore
	return s.aggregateStore.SavePositionAggregate(ctx, p)
}
