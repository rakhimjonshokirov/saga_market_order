package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"market_order/application/usecases"
	"market_order/domain/order"
	pkguuid "market_order/pkg/uuid"
)

// ===============================================
// STEP 4: SwapExecuted → Link Position → Complete Order
// ===============================================

// handleSwapExecuted processes SwapExecuted event
// Responsibilities:
// - Extract position_id from event metadata
// - Atomically complete order and update position
// - Publish PositionLinkedToOrder event
//
// CRITICAL: This step must be idempotent and retryable
// The swap has already been executed on blockchain, so we CANNOT compensate
// If this fails, we must retry until success or alert for manual intervention
func (s *OrderSagaRefactored) handleSwapExecuted(ctx context.Context, eventData []byte) error {
	log.Println("📨 [STEP 4] Saga: Received SwapExecuted event")

	var evt order.SwapExecuted
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	if processed, _ := s.processedEvents.IsProcessed(ctx, evt.EventID); processed {
		log.Printf("⏭️  Event %s already processed, skipping", evt.EventID)
		return nil
	}

	// Get position ID from event metadata (passed from STEP 3)
	positionID, ok := evt.Metadata["position_id"].(string)
	if !ok {
		log.Printf("❌ Position ID not found in event metadata")
		return fmt.Errorf("position_id not found in event metadata")
	}

	// Complete order and update position atomically
	log.Printf("✅ Completing order and updating position (atomic transaction)")

	if err := s.completeOrderUC.Execute(ctx, evt.AggregateID, positionID, usecases.SwapResult{
		TransactionHash: evt.TransactionHash,
		FromAmount:      evt.FromAmount,
		ToAmount:        evt.ToAmount,
		ExecutedPrice:   evt.ExecutedPrice,
		Fees:            evt.Fees,
		Slippage:        evt.Slippage,
	}); err != nil {
		log.Printf("❌ Failed to complete order: %v", err)
		// CRITICAL: Do NOT compensate here! Swap already executed.
		// Must retry or alert for manual intervention
		return err
	}

	// Publish PositionLinkedToOrder event
	linkedEvt := order.PositionLinkedToOrder{
		BaseEvent: order.BaseEvent{
			EventID:       pkguuid.New(),
			AggregateID:   evt.AggregateID,
			AggregateType: "Order",
			EventType:     "PositionLinkedToOrder",
			Version:       evt.Version + 1,
			Timestamp:     evt.Timestamp,
		},
		PositionID: positionID,
		OrderID:    evt.AggregateID,
	}

	eventBytes, _ := json.Marshal(linkedEvt)
	s.messageBus.Publish("PositionLinkedToOrder", eventBytes)

	// Mark as processed
	s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga-step4")

	log.Printf("🎉 ✅ [STEP 4] Completed: Order %s fully completed!", evt.AggregateID)
	return nil
}
