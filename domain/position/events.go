package position

import (
	"market_order/infrastructure/eventstore"
	"time"
)

type BaseEvent struct {
	EventID       string    `json:"event_id"`
	AggregateID   string    `json:"aggregate_id"`
	AggregateType string    `json:"aggregate_type"`
	EventType     string    `json:"event_type"`
	Version       int       `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
}

func (b BaseEvent) GetBaseFields() eventstore.BaseFields {
	return eventstore.BaseFields{
		EventID:       b.EventID,
		AggregateID:   b.AggregateID,
		AggregateType: b.AggregateType,
		EventType:     b.EventType,
		Version:       b.Version,
		Timestamp:     b.Timestamp,
	}
}

// PositionCreated - событие: позиция создана
type PositionCreated struct {
	BaseEvent
	UserID          string  `json:"user_id"`
	RemainingAmount float64 `json:"remaining_amount"`
	Status          string  `json:"status"` // "open"
}

func (e PositionCreated) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// PositionUpdated - событие: позиция обновлена
type PositionUpdated struct {
	BaseEvent
	AddedOrderID    string  `json:"added_order_id"`
	RemainingAmount float64 `json:"remaining_amount"`
	TotalValue      float64 `json:"total_value"`
	PnL             float64 `json:"pnl"`
}

func (e PositionUpdated) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// PositionClosed - событие: позиция закрыта
type PositionClosed struct {
	BaseEvent
	Reason   string    `json:"reason"`
	ClosedAt time.Time `json:"closed_at"`
}

func (e PositionClosed) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}
