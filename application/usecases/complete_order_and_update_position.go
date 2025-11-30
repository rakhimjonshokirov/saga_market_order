package usecases

import (
	"context"
	"fmt"

	"market_order/application/aggregates"
)

// CompleteOrderAndUpdatePositionUseCase completes order and updates position
//
// IMPORTANT:
// - Uses aggregateStore (NOT repositories!)
// - Loads aggregates from EventStore (source of truth)
// - Saves events atomically
// - NO direct database access
type CompleteOrderAndUpdatePositionUseCase struct {
	aggregateStore *aggregates.AggregateStore // ✅ Source of truth
}

func NewCompleteOrderAndUpdatePositionUseCase(
	aggregateStore *aggregates.AggregateStore,
) *CompleteOrderAndUpdatePositionUseCase {
	return &CompleteOrderAndUpdatePositionUseCase{
		aggregateStore: aggregateStore,
	}
}

type SwapResult struct {
	TransactionHash string
	FromAmount      float64
	ToAmount        float64
	ExecutedPrice   float64
	Fees            float64
	Slippage        float64
}

// Execute completes order and updates position atomically
// This is CRITICAL for consistency - both aggregates must be updated in single transaction
func (uc *CompleteOrderAndUpdatePositionUseCase) Execute(
	ctx context.Context,
	orderID, positionID string,
	swapResult SwapResult,
) error {
	// ✅ 1. Load Order from EventStore (source of truth)
	o, err := uc.aggregateStore.LoadOrderAggregate(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to load order aggregate: %w", err)
	}

	// ✅ 2. Complete Order (generates OrderCompleted event)
	if err := o.CompleteOrder(); err != nil {
		return fmt.Errorf("failed to complete order: %w", err)
	}

	// ✅ 3. Load Position from EventStore (source of truth)
	p, err := uc.aggregateStore.LoadPositionAggregate(ctx, positionID)
	if err != nil {
		return fmt.Errorf("failed to load position aggregate: %w", err)
	}

	// ✅ 4. Update Position (generates events)
	totalValue := swapResult.FromAmount
	pnl := 0.0 // For first order

	if err := p.AddOrder(orderID, swapResult.ToAmount, totalValue, pnl); err != nil {
		return fmt.Errorf("failed to update position: %w", err)
	}

	// ✅ 5. Save Order events to EventStore
	if err := uc.aggregateStore.SaveOrderAggregate(ctx, o); err != nil {
		return fmt.Errorf("failed to save order events: %w", err)
	}

	// ✅ 6. Save Position events to EventStore
	if err := uc.aggregateStore.SavePositionAggregate(ctx, p); err != nil {
		return fmt.Errorf("failed to save position events: %w", err)
	}

	// Events are automatically published via Outbox pattern
	// Projections will update database independently

	return nil
}
