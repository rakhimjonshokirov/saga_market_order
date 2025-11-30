package usecases

import (
	"context"
	"fmt"

	"market_order/application/aggregates"
	"market_order/domain/order"
)

// CreateOrderUseCase creates a new order
//
// IMPORTANT:
// - Uses aggregateStore (NOT repository!)
// - Creates new aggregate
// - Generates OrderAccepted event
// - Saves to EventStore
// - NO direct database access
type CreateOrderUseCase struct {
	aggregateStore *aggregates.AggregateStore // ✅ Source of truth
}

func NewCreateOrderUseCase(aggregateStore *aggregates.AggregateStore) *CreateOrderUseCase {
	return &CreateOrderUseCase{aggregateStore: aggregateStore}
}

type CreateOrderRequest struct {
	OrderID      string
	UserID       string
	FromAmount   float64
	FromCurrency string
	ToCurrency   string
	OrderType    string
}

func (uc *CreateOrderUseCase) Execute(ctx context.Context, req CreateOrderRequest) error {
	// ✅ Create new aggregate
	o := order.NewOrder()

	// ✅ Execute command (generates OrderAccepted event)
	err := o.AcceptOrder(
		req.OrderID,
		req.UserID,
		req.FromAmount,
		req.FromCurrency,
		req.ToCurrency,
		req.OrderType,
	)
	if err != nil {
		return err
	}

	fmt.Println("✅ OrderAccepted event generated:", req.OrderID)

	// ✅ Save events to EventStore (NOT repository!)
	if err := uc.aggregateStore.SaveOrderAggregate(ctx, o); err != nil {
		return fmt.Errorf("failed to save order events: %w", err)
	}

	// Events are automatically published via Outbox pattern
	// OrderProjection will create database record independently

	return nil
}
