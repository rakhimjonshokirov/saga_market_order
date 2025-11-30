package saga

import (
	"context"
	"encoding/json"
	"log"

	"market_order/domain/order"
	pkguuid "market_order/pkg/uuid"
)

// ===============================================
// STEP 3: PositionCreatedForOrder → Execute Swap → Publish SwapExecuted
// ===============================================

// handlePositionCreated processes PositionCreatedForOrder event
// Responsibilities:
// - Load order aggregate from EventStore
// - Execute blockchain swap via TradeWorker
// - Record swap execution result (generates SwapExecuted event)
// - Save events to EventStore
// - Publish SwapExecuted event with position_id (triggers STEP 4)
//
// This is the SLOWEST step (~5s) due to blockchain interaction
// Can be scaled independently with multiple workers
// NO repository usage - EventStore only!
func (s *OrderSagaRefactored) handlePositionCreated(ctx context.Context, eventData []byte) error {
	log.Println("📨 [STEP 3] Saga: Received PositionCreatedForOrder event")

	var evt order.PositionCreatedForOrder
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	if processed, _ := s.processedEvents.IsProcessed(ctx, evt.EventID); processed {
		log.Printf("⏭️  Event %s already processed, skipping", evt.EventID)
		return nil
	}

	// ✅ Load order aggregate from EventStore
	o, err := s.aggregateStore.LoadOrderAggregate(ctx, evt.AggregateID)
	if err != nil {
		return err
	}

	// Execute swap
	log.Printf("🔄 Executing swap for order %s", evt.AggregateID)

	idempotencyKey := generateIdempotencyKey(evt.AggregateID)

	// Mark as executing (generates SwapExecuting event)
	if err := o.StartSwapExecution(idempotencyKey); err != nil {
		return err
	}

	// ✅ Save events to EventStore
	if err := s.aggregateStore.SaveOrderAggregate(ctx, o); err != nil {
		return err
	}

	swapReq := SwapRequest{
		IdempotencyKey: idempotencyKey,
		FromCurrency:   o.FromCurrency,
		ToCurrency:     o.ToCurrency,
		FromAmount:     o.FromAmount,
		Slippage:       0.5, // 0.5%
	}

	swapResp, err := s.tradeWorker.ExecuteSwap(ctx, swapReq)
	if err != nil {
		log.Printf("❌ Swap execution failed: %v", err)
		return s.compensateSwapFailed(ctx, evt.AggregateID, evt.PositionID, err.Error())
	}

	log.Printf("✅ Swap executed: txHash=%s", swapResp.TransactionHash)

	// ✅ Reload aggregate and record swap execution
	o, _ = s.aggregateStore.LoadOrderAggregate(ctx, evt.AggregateID)
	o.RecordSwapExecution(
		swapResp.TransactionHash,
		o.FromAmount,
		swapResp.ToAmount,
		swapResp.ExecutedPrice,
		swapResp.Fees,
		swapResp.Slippage,
	)

	// ✅ Save events to EventStore
	if err := s.aggregateStore.SaveOrderAggregate(ctx, o); err != nil {
		return err
	}

	// Publish SwapExecuted with position_id in metadata
	// This will be published automatically by EventStore via Outbox
	// But we also manually publish for saga coordination
	swapExecutedEvt := order.SwapExecuted{
		BaseEvent: order.BaseEvent{
			EventID:       pkguuid.New(),
			AggregateID:   evt.AggregateID,
			AggregateType: "Order",
			EventType:     "SwapExecuted",
			Version:       o.Version,
			Timestamp:     o.UpdatedAt,
			Metadata: map[string]interface{}{
				"position_id": evt.PositionID, // Pass position ID to STEP 4
			},
		},
		TransactionHash: swapResp.TransactionHash,
		FromAmount:      o.FromAmount,
		ToAmount:        swapResp.ToAmount,
		ExecutedPrice:   swapResp.ExecutedPrice,
		Fees:            swapResp.Fees,
		Slippage:        swapResp.Slippage,
	}

	eventBytes, _ := json.Marshal(swapExecutedEvt)
	s.messageBus.Publish("SwapExecuted", eventBytes)

	// Mark as processed
	s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga-step3")

	// SwapExecuted event will trigger STEP 4
	log.Printf("✅ [STEP 3] Completed: Swap executed for order %s", evt.AggregateID)
	return nil
}
