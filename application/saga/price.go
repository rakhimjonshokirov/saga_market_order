package saga

import (
	"context"
	"encoding/json"
	"log"

	"market_order/domain/order"
	"market_order/domain/position"
	pkguuid "market_order/pkg/uuid"
)

// ===============================================
// STEP 2: PriceQuoted → Create Position → Publish PositionCreatedForOrder
// ===============================================

// handlePriceQuoted processes PriceQuoted event
// Responsibilities:
// - Create new position aggregate
// - Save position events to EventStore
// - Publish PositionCreatedForOrder event with position_id (triggers STEP 3)
// - NO repository usage - EventStore only!
func (s *OrderSagaRefactored) handlePriceQuoted(ctx context.Context, eventData []byte) error {
	log.Println("📨 [STEP 2] Saga: Received PriceQuoted event")

	var evt order.PriceQuoted
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	if processed, _ := s.processedEvents.IsProcessed(ctx, evt.EventID); processed {
		log.Printf("⏭️  Event %s already processed, skipping", evt.EventID)
		return nil
	}

	// ✅ Load order aggregate from EventStore to get user info
	o, err := s.aggregateStore.LoadOrderAggregate(ctx, evt.AggregateID)
	if err != nil {
		return err
	}

	// Create position
	log.Printf("📦 Creating position for user %s", o.UserID)
	positionID := pkguuid.New()

	// Create new position aggregate
	p := position.NewPosition()
	if err := p.CreatePosition(positionID, o.UserID); err != nil {
		return err
	}

	// ✅ Save position events to EventStore (not repository!)
	if err := s.aggregateStore.SavePositionAggregate(ctx, p); err != nil {
		return err
	}

	log.Printf("✅ Position created: %s", positionID)

	// Publish PositionCreatedForOrder event to trigger STEP 3
	// This is a saga coordination event (not an aggregate event)
	positionCreatedEvt := order.PositionCreatedForOrder{
		BaseEvent: order.BaseEvent{
			EventID:       pkguuid.New(),
			AggregateID:   evt.AggregateID, // order ID
			AggregateType: "Order",
			EventType:     "PositionCreatedForOrder",
			Version:       evt.Version + 1,
			Timestamp:     evt.Timestamp,
			Metadata: map[string]interface{}{
				"position_id": positionID, // Pass position ID for next steps
			},
		},
		PositionID: positionID,
		UserID:     o.UserID,
	}

	eventBytes, _ := json.Marshal(positionCreatedEvt)
	s.messageBus.Publish("PositionCreatedForOrder", eventBytes)

	// Mark as processed
	s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga-step2")

	log.Printf("✅ [STEP 2] Completed: Position created and linked to order %s", evt.AggregateID)
	return nil
}
