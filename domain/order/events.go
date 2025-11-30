package order

import (
	"time"
	"market_order/infrastructure/eventstore"
)

// BaseEvent содержит общие поля для всех событий
type BaseEvent struct {
	EventID       string                 `json:"event_id"`
	AggregateID   string                 `json:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type"`
	EventType     string                 `json:"event_type"`
	Version       int                    `json:"version"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// GetBaseFields extracts base fields from BaseEvent
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

// GetEventID returns the event ID
func (b BaseEvent) GetEventID() string {
	return b.EventID
}

// GetAggregateID returns the aggregate ID
func (b BaseEvent) GetAggregateID() string {
	return b.AggregateID
}

// GetVersion returns the version
func (b BaseEvent) GetVersion() int {
	return b.Version
}

// OrderAccepted - событие: заказ принят
type OrderAccepted struct {
	BaseEvent
	UserID       string  `json:"user_id"`
	FromAmount   float64 `json:"from_amount"`
	FromCurrency string  `json:"from_currency"`
	ToCurrency   string  `json:"to_currency"`
	OrderType    string  `json:"order_type"` // "market" или "limit"
}

// GetBaseEvent implements BaseFieldsProvider
func (e OrderAccepted) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// PriceQuoted - событие: получена котировка
type PriceQuoted struct {
	BaseEvent
	Price          float64   `json:"price"`
	ToAmount       float64   `json:"to_amount"`
	QuoteTimestamp time.Time `json:"quote_timestamp"`
}

func (e PriceQuoted) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// SwapExecuting - событие: начало исполнения swap
type SwapExecuting struct {
	BaseEvent
	IdempotencyKey string `json:"idempotency_key"`
}

func (e SwapExecuting) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// SwapExecuted - событие: swap исполнен
type SwapExecuted struct {
	BaseEvent
	TransactionHash string  `json:"transaction_hash"`
	FromAmount      float64 `json:"from_amount"`
	ToAmount        float64 `json:"to_amount"`
	ExecutedPrice   float64 `json:"executed_price"`
	Fees            float64 `json:"fees"`
	Slippage        float64 `json:"slippage"`
}

func (e SwapExecuted) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderCompleted - событие: заказ завершён
type OrderCompleted struct {
	BaseEvent
	FromAmount    float64 `json:"from_amount"`
	ToAmount      float64 `json:"to_amount"`
	ExecutedPrice float64 `json:"executed_price"`
	Status        string  `json:"status"` // "completed"
}

func (e OrderCompleted) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderFailed - событие: заказ провалился
type OrderFailed struct {
	BaseEvent
	Reason   string    `json:"reason"`
	FailedAt time.Time `json:"failed_at"`
}

func (e OrderFailed) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// ===============================================
// Additional Events for Enhanced Workflow
// ===============================================

// OrderInitialized - событие: ордер инициализирован
type OrderInitialized struct {
	BaseEvent
}

func (e OrderInitialized) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// LimitPriceSet - событие: установлена лимитная цена
type LimitPriceSet struct {
	BaseEvent
	LimitPrice float64 `json:"limit_price"`
}

func (e LimitPriceSet) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderUpdated - событие: ордер обновлён
type OrderUpdated struct {
	BaseEvent
	UpdatedFields map[string]interface{} `json:"updated_fields"`
}

func (e OrderUpdated) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderCancelled - событие: ордер отменён пользователем
type OrderCancelled struct {
	BaseEvent
	Reason      string    `json:"reason"`
	CancelledAt time.Time `json:"cancelled_at"`
}

func (e OrderCancelled) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// BalanceCheckPassed - событие: проверка баланса пройдена
type BalanceCheckPassed struct {
	BaseEvent
	AvailableAmount float64 `json:"available_amount"`
	Currency        string  `json:"currency"`
}

func (e BalanceCheckPassed) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// BalanceCheckFailed - событие: проверка баланса не пройдена
type BalanceCheckFailed struct {
	BaseEvent
	RequiredAmount  float64 `json:"required_amount"`
	AvailableAmount float64 `json:"available_amount"`
	Currency        string  `json:"currency"`
}

func (e BalanceCheckFailed) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderPlacedInBook - событие: ордер размещён в книге заявок
type OrderPlacedInBook struct {
	BaseEvent
	OrderBookID string    `json:"order_book_id"`
	PlacedAt    time.Time `json:"placed_at"`
}

func (e OrderPlacedInBook) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// OrderPartiallyFilled - событие: ордер частично исполнен
type OrderPartiallyFilled struct {
	BaseEvent
	FilledAmount    float64   `json:"filled_amount"`
	ExecutedPrice   float64   `json:"executed_price"`
	TransactionHash string    `json:"transaction_hash"`
	FilledAt        time.Time `json:"filled_at"`
}

func (e OrderPartiallyFilled) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// ===============================================
// Saga Step Events
// ===============================================

// PositionCreatedForOrder - событие: позиция создана для ордера (saga step)
type PositionCreatedForOrder struct {
	BaseEvent
	PositionID string `json:"position_id"`
	UserID     string `json:"user_id"`
}

func (e PositionCreatedForOrder) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}

// PositionLinkedToOrder - событие: позиция привязана к ордеру (saga step)
type PositionLinkedToOrder struct {
	BaseEvent
	PositionID string `json:"position_id"`
	OrderID    string `json:"order_id"`
}

func (e PositionLinkedToOrder) GetBaseEvent() eventstore.BaseFields {
	return e.BaseEvent.GetBaseFields()
}
