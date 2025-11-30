package saga

import (
	"context"
	"encoding/json"
	"log"

	"market_order/domain/order"
)

// ===============================================
// STEP 1: OrderAccepted → Get Price → Publish PriceQuoted
// ===============================================

// handleOrderAccepted processes OrderAccepted event
// Responsibilities:
// - Get market price from price service
// - Load order aggregate from EventStore (source of truth)
// - Update order with quoted price (generates PriceQuoted event)
// - Save events to EventStore
// - Events are automatically published via Outbox pattern
func (s *OrderSagaRefactored) handleOrderAccepted(ctx context.Context, eventData []byte) error {
	log.Println("📨 [STEP 1] Saga: Received OrderAccepted event")

	var evt order.OrderAccepted
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	if processed, _ := s.processedEvents.IsProcessed(ctx, evt.EventID); processed {
		log.Printf("⏭️  Event %s already processed, skipping", evt.EventID)
		return nil
	}

	// Get market price
	log.Printf("📊 Getting market price for %s/%s", evt.FromCurrency, evt.ToCurrency)
	price, err := s.priceService.GetMarketPrice(ctx, evt.FromCurrency, evt.ToCurrency)
	if err != nil {
		log.Printf("❌ Failed to get price: %v", err)
		return s.compensateOrderFailed(ctx, evt.AggregateID, "price_unavailable")
	}

	toAmount := evt.FromAmount / price
	log.Printf("✅ Price quoted: 1 %s = %.2f %s, toAmount = %.8f",
		evt.ToCurrency, price, evt.FromCurrency, toAmount)

	// ✅ Load aggregate from EventStore (source of truth!)
	o, err := s.aggregateStore.LoadOrderAggregate(ctx, evt.AggregateID)
	if err != nil {
		return err
	}

	// Generate PriceQuoted event
	if err := o.QuotePrice(price, toAmount); err != nil {
		return err
	}

	// ✅ Save events to EventStore (not repository!)
	if err := s.aggregateStore.SaveOrderAggregate(ctx, o); err != nil {
		return err
	}

	// Mark as processed
	s.processedEvents.MarkAsProcessed(ctx, evt.EventID, evt.AggregateID, evt.EventType, "order-saga-step1")

	// PriceQuoted event will be published automatically via Outbox
	// and trigger STEP 2
	log.Printf("✅ [STEP 1] Completed: Price quoted for order %s", evt.AggregateID)
	return nil
}
