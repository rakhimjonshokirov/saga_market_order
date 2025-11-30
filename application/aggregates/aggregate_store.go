package aggregates

import (
	"context"
	"encoding/json"
	"fmt"

	"market_order/domain/order"
	"market_order/domain/position"
	"market_order/infrastructure/eventstore"
)

// AggregateStore provides high-level methods for loading and saving aggregates
type AggregateStore struct {
	eventStore eventstore.EventStore
}

func NewAggregateStore(es eventstore.EventStore) *AggregateStore {
	return &AggregateStore{eventStore: es}
}

// LoadOrderAggregate loads an Order aggregate from events
func (as *AggregateStore) LoadOrderAggregate(ctx context.Context, aggregateID string) (*order.Order, error) {
	events, err := as.eventStore.Load(ctx, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("aggregate not found: %s", aggregateID)
	}

	// Create new aggregate
	o := order.NewOrder()

	// Replay all events
	for _, evt := range events {
		domainEvent, err := deserializeOrderEvent(evt)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize event: %w", err)
		}

		// Apply event to rebuild state
		if err := o.When(domainEvent); err != nil {
			return nil, fmt.Errorf("failed to apply event: %w", err)
		}
	}

	return o, nil
}

// SaveOrderAggregate saves Order aggregate changes (uncommitted events)
func (as *AggregateStore) SaveOrderAggregate(ctx context.Context, o *order.Order) error {
	if len(o.Changes) == 0 {
		return nil // No changes to save
	}

	// Save events to EventStore
	if err := as.eventStore.Save(ctx, o.Changes); err != nil {
		return fmt.Errorf("failed to save events: %w", err)
	}

	// Clear uncommitted events after successful save
	o.Changes = make([]interface{}, 0)

	return nil
}

// LoadPositionAggregate loads a Position aggregate from events
func (as *AggregateStore) LoadPositionAggregate(ctx context.Context, aggregateID string) (*position.Position, error) {
	events, err := as.eventStore.Load(ctx, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("aggregate not found: %s", aggregateID)
	}

	// Create new aggregate
	p := position.NewPosition()

	// Replay all events
	for _, evt := range events {
		domainEvent, err := deserializePositionEvent(evt)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize event: %w", err)
		}

		// Apply event to rebuild state
		if err := p.When(domainEvent); err != nil {
			return nil, fmt.Errorf("failed to apply event: %w", err)
		}
	}

	return p, nil
}

// SavePositionAggregate saves Position aggregate changes
func (as *AggregateStore) SavePositionAggregate(ctx context.Context, p *position.Position) error {
	if len(p.Changes) == 0 {
		return nil
	}

	if err := as.eventStore.Save(ctx, p.Changes); err != nil {
		return fmt.Errorf("failed to save events: %w", err)
	}

	p.Changes = make([]interface{}, 0)
	return nil
}

// deserializeOrderEvent converts stored event to domain event
func deserializeOrderEvent(evt eventstore.Event) (interface{}, error) {
	switch evt.EventType {
	case "OrderAccepted":
		var e order.OrderAccepted
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "PriceQuoted":
		var e order.PriceQuoted
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "SwapExecuting":
		var e order.SwapExecuting
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "SwapExecuted":
		var e order.SwapExecuted
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "OrderCompleted":
		var e order.OrderCompleted
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "OrderFailed":
		var e order.OrderFailed
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", evt.EventType)
	}
}

// deserializePositionEvent converts stored event to domain event
func deserializePositionEvent(evt eventstore.Event) (interface{}, error) {
	switch evt.EventType {
	case "PositionCreated":
		var e position.PositionCreated
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "PositionClosed":
		var e position.PositionClosed
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	default:
		return nil, fmt.Errorf("unknown event type: %s", evt.EventType)
	}
}
