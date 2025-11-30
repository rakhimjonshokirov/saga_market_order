package orderbook

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// OrderBookStatus представляет статус книги заявок
type OrderBookStatus string

const (
	OrderBookStatusActive   OrderBookStatus = "active"
	OrderBookStatusSuspended OrderBookStatus = "suspended"
	OrderBookStatusClosed   OrderBookStatus = "closed"
)

// LimitOrder представляет лимитный ордер в книге
type LimitOrder struct {
	OrderID       string
	UserID        string
	Price         float64
	Amount        float64
	Side          string // "buy" или "sell"
	PlacedAt      time.Time
	RemainingAmount float64
}

// OrderBook - агрегат книги заявок (matching engine)
type OrderBook struct {
	ID            string
	TradingPair   string // например "BTC/USDT"
	BuyOrders     []LimitOrder
	SellOrders    []LimitOrder
	LastPrice     float64
	Status        OrderBookStatus
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Несохранённые события
	Changes []interface{}
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		BuyOrders:  make([]LimitOrder, 0),
		SellOrders: make([]LimitOrder, 0),
		Changes:    make([]interface{}, 0),
	}
}

// When восстанавливает состояние
func (ob *OrderBook) When(event interface{}) error {
	switch e := event.(type) {

	case OrderBookCreated:
		ob.ID = e.AggregateID
		ob.TradingPair = e.TradingPair
		ob.Status = OrderBookStatusActive
		ob.Version = e.Version
		ob.CreatedAt = e.Timestamp
		ob.UpdatedAt = e.Timestamp

	case LimitOrderAdded:
		order := LimitOrder{
			OrderID:         e.OrderID,
			UserID:          e.UserID,
			Price:           e.Price,
			Amount:          e.Amount,
			Side:            e.Side,
			PlacedAt:        e.PlacedAt,
			RemainingAmount: e.Amount,
		}

		if e.Side == "buy" {
			ob.BuyOrders = append(ob.BuyOrders, order)
			// Sort buy orders: highest price first
			sort.Slice(ob.BuyOrders, func(i, j int) bool {
				return ob.BuyOrders[i].Price > ob.BuyOrders[j].Price
			})
		} else {
			ob.SellOrders = append(ob.SellOrders, order)
			// Sort sell orders: lowest price first
			sort.Slice(ob.SellOrders, func(i, j int) bool {
				return ob.SellOrders[i].Price < ob.SellOrders[j].Price
			})
		}
		ob.Version = e.Version
		ob.UpdatedAt = e.Timestamp

	case OrdersMatched:
		// Remove or update matched orders
		ob.removeOrUpdateOrder(e.BuyOrderID, e.MatchedAmount, "buy")
		ob.removeOrUpdateOrder(e.SellOrderID, e.MatchedAmount, "sell")
		ob.LastPrice = e.MatchedPrice
		ob.Version = e.Version
		ob.UpdatedAt = e.Timestamp

	case LimitOrderCancelled:
		ob.removeOrder(e.OrderID, e.Side)
		ob.Version = e.Version
		ob.UpdatedAt = e.Timestamp

	case PriceUpdated:
		ob.LastPrice = e.NewPrice
		ob.Version = e.Version
		ob.UpdatedAt = e.Timestamp

	default:
		return fmt.Errorf("unknown event type: %T", event)
	}

	return nil
}

func (ob *OrderBook) Apply(event interface{}) error {
	if err := ob.When(event); err != nil {
		return err
	}
	ob.Changes = append(ob.Changes, event)
	return nil
}

// ===============================================
// Commands
// ===============================================

// CreateOrderBook - команда: создать книгу заявок
func (ob *OrderBook) CreateOrderBook(orderBookID, tradingPair string) error {
	event := OrderBookCreated{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   orderBookID,
			AggregateType: "OrderBook",
			EventType:     "OrderBookCreated",
			Version:       1,
			Timestamp:     time.Now(),
		},
		TradingPair: tradingPair,
	}

	return ob.Apply(event)
}

// AddLimitOrder - команда: добавить лимитный ордер
func (ob *OrderBook) AddLimitOrder(orderID, userID string, price, amount float64, side string) error {
	if ob.Status != OrderBookStatusActive {
		return fmt.Errorf("order book is %s", ob.Status)
	}

	if side != "buy" && side != "sell" {
		return errors.New("side must be 'buy' or 'sell'")
	}

	if price <= 0 || amount <= 0 {
		return errors.New("price and amount must be positive")
	}

	event := LimitOrderAdded{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   ob.ID,
			AggregateType: "OrderBook",
			EventType:     "LimitOrderAdded",
			Version:       ob.Version + 1,
			Timestamp:     time.Now(),
		},
		OrderID:  orderID,
		UserID:   userID,
		Price:    price,
		Amount:   amount,
		Side:     side,
		PlacedAt: time.Now(),
	}

	return ob.Apply(event)
}

// MatchOrders - команда: провести матчинг ордеров
func (ob *OrderBook) MatchOrders() error {
	if ob.Status != OrderBookStatusActive {
		return fmt.Errorf("order book is %s", ob.Status)
	}

	// Simple matching algorithm: check if best buy >= best sell
	if len(ob.BuyOrders) == 0 || len(ob.SellOrders) == 0 {
		return nil // Nothing to match
	}

	bestBuy := ob.BuyOrders[0]
	bestSell := ob.SellOrders[0]

	if bestBuy.Price >= bestSell.Price {
		// Match found!
		matchedAmount := min(bestBuy.RemainingAmount, bestSell.RemainingAmount)
		matchedPrice := (bestBuy.Price + bestSell.Price) / 2.0

		event := OrdersMatched{
			BaseEvent: BaseEvent{
				EventID:       generateUUID(),
				AggregateID:   ob.ID,
				AggregateType: "OrderBook",
				EventType:     "OrdersMatched",
				Version:       ob.Version + 1,
				Timestamp:     time.Now(),
			},
			BuyOrderID:    bestBuy.OrderID,
			SellOrderID:   bestSell.OrderID,
			MatchedPrice:  matchedPrice,
			MatchedAmount: matchedAmount,
			MatchedAt:     time.Now(),
		}

		return ob.Apply(event)
	}

	return nil
}

// CancelLimitOrder - команда: отменить лимитный ордер
func (ob *OrderBook) CancelLimitOrder(orderID, side string) error {
	if ob.Status != OrderBookStatusActive {
		return fmt.Errorf("order book is %s", ob.Status)
	}

	// Check if order exists
	found := false
	if side == "buy" {
		for _, order := range ob.BuyOrders {
			if order.OrderID == orderID {
				found = true
				break
			}
		}
	} else {
		for _, order := range ob.SellOrders {
			if order.OrderID == orderID {
				found = true
				break
			}
		}
	}

	if !found {
		return errors.New("order not found in order book")
	}

	event := LimitOrderCancelled{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   ob.ID,
			AggregateType: "OrderBook",
			EventType:     "LimitOrderCancelled",
			Version:       ob.Version + 1,
			Timestamp:     time.Now(),
		},
		OrderID:     orderID,
		Side:        side,
		CancelledAt: time.Now(),
	}

	return ob.Apply(event)
}

// UpdatePrice - команда: обновить текущую цену (из WebSocket feed)
func (ob *OrderBook) UpdatePrice(newPrice float64, source string) error {
	if newPrice <= 0 {
		return errors.New("price must be positive")
	}

	event := PriceUpdated{
		BaseEvent: BaseEvent{
			EventID:       generateUUID(),
			AggregateID:   ob.ID,
			AggregateType: "OrderBook",
			EventType:     "PriceUpdated",
			Version:       ob.Version + 1,
			Timestamp:     time.Now(),
		},
		NewPrice:  newPrice,
		OldPrice:  ob.LastPrice,
		Source:    source,
		UpdatedAt: time.Now(),
	}

	return ob.Apply(event)
}

// ===============================================
// Helper methods
// ===============================================

func (ob *OrderBook) removeOrUpdateOrder(orderID string, matchedAmount float64, side string) {
	if side == "buy" {
		for i, order := range ob.BuyOrders {
			if order.OrderID == orderID {
				order.RemainingAmount -= matchedAmount
				if order.RemainingAmount <= 0 {
					// Remove order
					ob.BuyOrders = append(ob.BuyOrders[:i], ob.BuyOrders[i+1:]...)
				} else {
					ob.BuyOrders[i] = order
				}
				break
			}
		}
	} else {
		for i, order := range ob.SellOrders {
			if order.OrderID == orderID {
				order.RemainingAmount -= matchedAmount
				if order.RemainingAmount <= 0 {
					// Remove order
					ob.SellOrders = append(ob.SellOrders[:i], ob.SellOrders[i+1:]...)
				} else {
					ob.SellOrders[i] = order
				}
				break
			}
		}
	}
}

func (ob *OrderBook) removeOrder(orderID, side string) {
	if side == "buy" {
		for i, order := range ob.BuyOrders {
			if order.OrderID == orderID {
				ob.BuyOrders = append(ob.BuyOrders[:i], ob.BuyOrders[i+1:]...)
				break
			}
		}
	} else {
		for i, order := range ob.SellOrders {
			if order.OrderID == orderID {
				ob.SellOrders = append(ob.SellOrders[:i], ob.SellOrders[i+1:]...)
				break
			}
		}
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func generateUUID() string {
	return fmt.Sprintf("uuid-%d", time.Now().UnixNano())
}
