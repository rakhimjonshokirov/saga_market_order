package orderbook

import "time"

type BaseEvent struct {
	EventID       string    `json:"event_id"`
	AggregateID   string    `json:"aggregate_id"`
	AggregateType string    `json:"aggregate_type"`
	EventType     string    `json:"event_type"`
	Version       int       `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
}

func (b BaseEvent) GetBaseFields() BaseFields {
	return BaseFields{
		EventID:       b.EventID,
		AggregateID:   b.AggregateID,
		AggregateType: b.AggregateType,
		EventType:     b.EventType,
		Version:       b.Version,
		Timestamp:     b.Timestamp,
	}
}

type BaseFields struct {
	EventID       string
	AggregateID   string
	AggregateType string
	EventType     string
	Version       int
	Timestamp     time.Time
}

// OrderBookCreated - событие: книга заявок создана
type OrderBookCreated struct {
	BaseEvent
	TradingPair string `json:"trading_pair"` // "BTC/USDT"
}

// LimitOrderAdded - событие: лимитный ордер добавлен
type LimitOrderAdded struct {
	BaseEvent
	OrderID  string    `json:"order_id"`
	UserID   string    `json:"user_id"`
	Price    float64   `json:"price"`
	Amount   float64   `json:"amount"`
	Side     string    `json:"side"` // "buy" or "sell"
	PlacedAt time.Time `json:"placed_at"`
}

// OrdersMatched - событие: ордера сматчились
type OrdersMatched struct {
	BaseEvent
	BuyOrderID    string    `json:"buy_order_id"`
	SellOrderID   string    `json:"sell_order_id"`
	MatchedPrice  float64   `json:"matched_price"`
	MatchedAmount float64   `json:"matched_amount"`
	MatchedAt     time.Time `json:"matched_at"`
}

// LimitOrderCancelled - событие: лимитный ордер отменён
type LimitOrderCancelled struct {
	BaseEvent
	OrderID     string    `json:"order_id"`
	Side        string    `json:"side"`
	CancelledAt time.Time `json:"cancelled_at"`
}

// PriceUpdated - событие: цена обновлена (от WebSocket feed)
type PriceUpdated struct {
	BaseEvent
	NewPrice  float64   `json:"new_price"`
	OldPrice  float64   `json:"old_price"`
	Source    string    `json:"source"` // "binance", "uniswap", etc.
	UpdatedAt time.Time `json:"updated_at"`
}

// GetBaseEvent implementations
func (e OrderBookCreated) GetBaseEvent() BaseFields {
	return e.BaseEvent.GetBaseFields()
}

func (e LimitOrderAdded) GetBaseEvent() BaseFields {
	return e.BaseEvent.GetBaseFields()
}

func (e OrdersMatched) GetBaseEvent() BaseFields {
	return e.BaseEvent.GetBaseFields()
}

func (e LimitOrderCancelled) GetBaseEvent() BaseFields {
	return e.BaseEvent.GetBaseFields()
}

func (e PriceUpdated) GetBaseEvent() BaseFields {
	return e.BaseEvent.GetBaseFields()
}
