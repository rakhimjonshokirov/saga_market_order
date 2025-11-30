package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"market_order/domain/position"
	"market_order/infrastructure/eventstore"
)

type PositionRepository struct {
	eventStore eventstore.EventStore
}

func NewPositionRepository(es eventstore.EventStore) *PositionRepository {
	return &PositionRepository{eventStore: es}
}

func (r *PositionRepository) Get(ctx context.Context, positionID string) (*position.Position, error) {
	events, err := r.eventStore.Load(ctx, positionID)
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, errors.New("position not found")
	}

	p := position.NewPosition()

	for _, evt := range events {
		domainEvent, err := deserializePositionEvent(evt)
		if err != nil {
			return nil, err
		}

		if err := p.When(domainEvent); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func (r *PositionRepository) Save(ctx context.Context, p *position.Position) error {
	if len(p.Changes) == 0 {
		return nil
	}

	if err := r.eventStore.Save(ctx, p.Changes); err != nil {
		return err
	}

	p.Changes = nil
	return nil
}

func deserializePositionEvent(evt eventstore.Event) (interface{}, error) {
	switch evt.EventType {
	case "PositionCreated":
		var e position.PositionCreated
		if err := json.Unmarshal(evt.EventData, &e); err != nil {
			return nil, err
		}
		return e, nil

	case "PositionUpdated":
		var e position.PositionUpdated
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
