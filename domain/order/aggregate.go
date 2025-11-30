package order

import (
	"errors"
	"fmt"
	"time"
)

// OrderStatus представляет статус заказа
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusExecuting OrderStatus = "executing"
	OrderStatusCompleted OrderStatus = "completed"
	OrderStatusFailed    OrderStatus = "failed"
)

// Order - агрегат заказа
type Order struct {
	// Состояние
	ID            string
	UserID        string
	FromAmount    float64
	FromCurrency  string
	ToCurrency    string
	ToAmount      float64
	ExecutedPrice float64
	OrderType     string // "market" или "limit"
	Status        OrderStatus
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Несохранённые события
	Changes []interface{}
}

// NewOrder создаёт новый пустой заказ
func NewOrder() *Order {
	return &Order{
		Changes: make([]interface{}, 0),
	}
}

// When восстанавливает состояние из события (replay)
func (o *Order) When(event interface{}) error {
	switch e := event.(type) {

	case OrderAccepted:
		o.ID = e.AggregateID
		o.UserID = e.UserID
		o.FromAmount = e.FromAmount
		o.FromCurrency = e.FromCurrency
		o.ToCurrency = e.ToCurrency
		o.OrderType = e.OrderType
		o.Status = OrderStatusPending
		o.Version = e.Version
		o.CreatedAt = e.Timestamp
		o.UpdatedAt = e.Timestamp

	case PriceQuoted:
		o.ToAmount = e.ToAmount
		o.ExecutedPrice = e.Price
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case SwapExecuting:
		o.Status = OrderStatusExecuting
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case SwapExecuted:
		o.ToAmount = e.ToAmount
		o.ExecutedPrice = e.ExecutedPrice
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderCompleted:
		o.Status = OrderStatusCompleted
		o.FromAmount = e.FromAmount
		o.ToAmount = e.ToAmount
		o.ExecutedPrice = e.ExecutedPrice
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderFailed:
		o.Status = OrderStatusFailed
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderInitialized:
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case LimitPriceSet:
		o.ExecutedPrice = e.LimitPrice
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderUpdated:
		for key, value := range e.UpdatedFields {
			switch key {
			case "from_amount":
				if v, ok := value.(float64); ok {
					o.FromAmount = v
				}
			case "to_amount":
				if v, ok := value.(float64); ok {
					o.ToAmount = v
				}
			}
		}
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderCancelled:
		o.Status = OrderStatusFailed
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case BalanceCheckPassed:
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case BalanceCheckFailed:
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderPlacedInBook:
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	case OrderPartiallyFilled:
		o.ToAmount += e.FilledAmount
		o.ExecutedPrice = e.ExecutedPrice
		o.Version = e.Version
		o.UpdatedAt = e.Timestamp

	default:
		return fmt.Errorf("unknown event type: %T", event)
	}

	return nil
}

// Apply применяет событие и добавляет в Changes
func (o *Order) Apply(event interface{}) error {
	if err := o.When(event); err != nil {
		return err
	}

	o.Changes = append(o.Changes, event)
	return nil
}

// AcceptOrder - команда: принять заказ
func (o *Order) AcceptOrder(
	orderID, userID string,
	fromAmount float64,
	fromCurrency, toCurrency string,
	orderType string,
) error {
	// Бизнес-валидация
	if fromAmount <= 0 {
		return errors.New("from_amount must be positive")
	}

	if fromAmount < 10.0 {
		return errors.New("minimum order amount is 10")
	}

	if orderType != "market" && orderType != "limit" {
		return errors.New("order_type must be 'market' or 'limit'")
	}

	// Генерируем событие
	event := OrderAccepted{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   orderID,
			AggregateType: "Order",
			EventType:     "OrderAccepted",
			Version:       1,
			Timestamp:     time.Now(),
			Metadata: map[string]interface{}{
				"user_agent": "api-v1",
			},
		},
		UserID:       userID,
		FromAmount:   fromAmount,
		FromCurrency: fromCurrency,
		ToCurrency:   toCurrency,
		OrderType:    orderType,
	}

	return o.Apply(event)
}

// QuotePrice - команда: установить котировку
func (o *Order) QuotePrice(price, toAmount float64) error {
	// Бизнес-правила
	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot quote price: order status is %s", o.Status)
	}

	if price <= 0 || toAmount <= 0 {
		return errors.New("price and toAmount must be positive")
	}

	event := PriceQuoted{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "PriceQuoted",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		Price:          price,
		ToAmount:       toAmount,
		QuoteTimestamp: time.Now(),
	}

	return o.Apply(event)
}

// StartSwapExecution - команда: начать исполнение
func (o *Order) StartSwapExecution(idempotencyKey string) error {
	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot start execution: order status is %s", o.Status)
	}

	event := SwapExecuting{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "SwapExecuting",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		IdempotencyKey: idempotencyKey,
	}

	return o.Apply(event)
}

// RecordSwapExecution - команда: записать результат swap
func (o *Order) RecordSwapExecution(
	txHash string,
	fromAmount, toAmount, executedPrice, fees, slippage float64,
) error {
	if o.Status != OrderStatusExecuting {
		return fmt.Errorf("cannot record execution: order status is %s", o.Status)
	}

	event := SwapExecuted{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "SwapExecuted",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		TransactionHash: txHash,
		FromAmount:      fromAmount,
		ToAmount:        toAmount,
		ExecutedPrice:   executedPrice,
		Fees:            fees,
		Slippage:        slippage,
	}

	return o.Apply(event)
}

// CompleteOrder - команда: завершить заказ
func (o *Order) CompleteOrder() error {
	// Идемпотентность на бизнес-уровне
	if o.Status == OrderStatusCompleted {
		return nil // Уже завершён, ничего не делаем
	}

	if o.Status != OrderStatusExecuting {
		return fmt.Errorf("cannot complete order: order status is %s", o.Status)
	}

	event := OrderCompleted{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderCompleted",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		FromAmount:    o.FromAmount,
		ToAmount:      o.ToAmount,
		ExecutedPrice: o.ExecutedPrice,
		Status:        "completed",
	}

	return o.Apply(event)
}

// FailOrder - команда: провалить заказ (компенсация)
func (o *Order) FailOrder(reason string) error {
	// Идемпотентность
	if o.Status == OrderStatusFailed {
		return nil
	}

	if o.Status == OrderStatusCompleted {
		return errors.New("cannot fail completed order")
	}

	event := OrderFailed{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderFailed",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		Reason:   reason,
		FailedAt: time.Now(),
	}

	return o.Apply(event)
}

// ===============================================
// Additional Commands for Enhanced Workflow
// ===============================================

// InitializeOrder - команда: инициализация ордера (загрузка данных)
func (o *Order) InitializeOrder() error {
	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot initialize: order status is %s", o.Status)
	}

	event := OrderInitialized{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderInitialized",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
	}

	return o.Apply(event)
}

// SetLimitPrice - команда: установка лимитной цены
func (o *Order) SetLimitPrice(limitPrice float64) error {
	if o.OrderType != "limit" {
		return errors.New("cannot set limit price: order is not a limit order")
	}

	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot set limit price: order status is %s", o.Status)
	}

	if limitPrice <= 0 {
		return errors.New("limit price must be positive")
	}

	event := LimitPriceSet{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "LimitPriceSet",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		LimitPrice: limitPrice,
	}

	return o.Apply(event)
}

// UpdateOrder - команда: обновление параметров ордера
func (o *Order) UpdateOrder(params map[string]interface{}) error {
	if o.Status == OrderStatusCompleted {
		return errors.New("cannot update completed order")
	}

	if o.Status == OrderStatusFailed {
		return errors.New("cannot update failed order")
	}

	event := OrderUpdated{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderUpdated",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		UpdatedFields: params,
	}

	return o.Apply(event)
}

// CancelOrder - команда: отмена ордера пользователем
func (o *Order) CancelOrder(reason string) error {
	// Idempotency check
	if o.Status == OrderStatusFailed {
		return nil // Already cancelled/failed
	}

	if o.Status == OrderStatusCompleted {
		return errors.New("cannot cancel completed order")
	}

	if o.Status == OrderStatusExecuting {
		return errors.New("cannot cancel executing order")
	}

	event := OrderCancelled{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderCancelled",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		Reason:      reason,
		CancelledAt: time.Now(),
	}

	return o.Apply(event)
}

// CheckBalances - команда: проверка достаточности средств
func (o *Order) CheckBalances(availableBalance float64) error {
	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot check balances: order status is %s", o.Status)
	}

	if availableBalance < o.FromAmount {
		// Insufficient balance
		event := BalanceCheckFailed{
			BaseEvent: BaseEvent{
				EventID:       generateUUID(),
				AggregateID:   o.ID,
				AggregateType: "Order",
				EventType:     "BalanceCheckFailed",
				Version:       o.Version + 1,
				Timestamp:     time.Now(),
			},
			RequiredAmount:  o.FromAmount,
			AvailableAmount: availableBalance,
			Currency:        o.FromCurrency,
		}
		return o.Apply(event)
	}

	// Balance is sufficient
	event := BalanceCheckPassed{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "BalanceCheckPassed",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		AvailableAmount: availableBalance,
		Currency:        o.FromCurrency,
	}

	return o.Apply(event)
}

// PlaceInOrderBook - команда: размещение лимитного ордера в книге заявок
func (o *Order) PlaceInOrderBook(orderBookID string) error {
	if o.OrderType != "limit" {
		return errors.New("only limit orders can be placed in order book")
	}

	if o.Status != OrderStatusPending {
		return fmt.Errorf("cannot place in order book: order status is %s", o.Status)
	}

	event := OrderPlacedInBook{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderPlacedInBook",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		OrderBookID: orderBookID,
		PlacedAt:    time.Now(),
	}

	return o.Apply(event)
}

// PartiallyFill - команда: частичное исполнение (для лимитных ордеров)
func (o *Order) PartiallyFill(filledAmount, executedPrice float64, transactionHash string) error {
	if o.Status != OrderStatusExecuting {
		return fmt.Errorf("cannot partially fill: order status is %s", o.Status)
	}

	if filledAmount <= 0 || filledAmount > o.FromAmount {
		return errors.New("invalid filled amount")
	}

	event := OrderPartiallyFilled{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   o.ID,
			AggregateType: "Order",
			EventType:     "OrderPartiallyFilled",
			Version:       o.Version + 1,
			Timestamp:     time.Now(),
		},
		FilledAmount:    filledAmount,
		ExecutedPrice:   executedPrice,
		TransactionHash: transactionHash,
		FilledAt:        time.Now(),
	}

	return o.Apply(event)
}
