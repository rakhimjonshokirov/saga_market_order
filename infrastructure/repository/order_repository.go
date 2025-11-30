package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"market_order/domain/order"
	"market_order/infrastructure/eventstore"
)

type OrderRepository struct {
	eventStore eventstore.EventStore
}

func NewOrderRepository(es eventstore.EventStore) *OrderRepository {
	return &OrderRepository{eventStore: es}
}

// Get восстанавливает Order aggregate из Event Store
func (r *OrderRepository) Get(ctx context.Context, orderID string) (*order.Order, error) {
	// Загружаем события
	events, err := r.eventStore.Load(ctx, orderID)
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, errors.New("order not found")
	}

	// Создаём пустой агрегат
	o := order.NewOrder()

	// Восстанавливаем состояние, применяя события
	for _, evt := range events {
		domainEvent, err := deserializeOrderEvent(evt)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize event: %w", err)
		}

		if err := o.When(domainEvent); err != nil {
			return nil, fmt.Errorf("failed to apply event: %w", err)
		}
	}

	return o, nil
}

// Save сохраняет новые события
func (r *OrderRepository) Save(ctx context.Context, o *order.Order) error {
	if len(o.Changes) == 0 {
		return nil // Нечего сохранять
	}

	// Сохраняем в Event Store
	if err := r.eventStore.Save(ctx, o.Changes); err != nil {
		return err
	}

	// Очищаем Changes после успешного сохранения
	o.Changes = nil

	return nil
}

// deserializeOrderEvent конвертирует сохранённое событие в доменное
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
