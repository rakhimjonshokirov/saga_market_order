package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"market_order/application/usecases"
	"market_order/infrastructure/eventstore"
	pkguuid "market_order/pkg/uuid"
)

// OrderHandler handles HTTP requests for orders
type OrderHandler struct {
	createOrderUC *usecases.CreateOrderUseCase
	eventStore    eventstore.EventStore // For reading event history
}

func NewOrderHandler(
	createOrderUC *usecases.CreateOrderUseCase,
	eventStore eventstore.EventStore,
) *OrderHandler {
	return &OrderHandler{
		createOrderUC: createOrderUC,
		eventStore:    eventStore,
	}
}

// CreateOrderRequest is the HTTP request body for creating an order
type CreateOrderRequest struct {
	UserID       string  `json:"user_id"`
	FromAmount   float64 `json:"from_amount"`
	FromCurrency string  `json:"from_currency"`
	ToCurrency   string  `json:"to_currency"`
	OrderType    string  `json:"order_type"` // "market" or "limit"
}

// CreateOrderResponse is the HTTP response
type CreateOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// CreateOrder handles POST /orders
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if req.FromAmount <= 0 {
		http.Error(w, "from_amount must be positive", http.StatusBadRequest)
		return
	}
	if req.FromCurrency == "" || req.ToCurrency == "" {
		http.Error(w, "from_currency and to_currency are required", http.StatusBadRequest)
		return
	}
	if req.OrderType == "" {
		req.OrderType = "market" // Default to market order
	}

	// Generate order ID
	orderID := pkguuid.New()

	// Execute use case
	ctx := context.Background()
	err := h.createOrderUC.Execute(ctx, usecases.CreateOrderRequest{
		OrderID:      orderID,
		UserID:       req.UserID,
		FromAmount:   req.FromAmount,
		FromCurrency: req.FromCurrency,
		ToCurrency:   req.ToCurrency,
		OrderType:    req.OrderType,
	})

	if err != nil {
		log.Printf("Failed to create order: %v", err)
		http.Error(w, "Failed to create order: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return response
	resp := CreateOrderResponse{
		OrderID: orderID,
		Status:  "pending",
		Message: "Order accepted and will be processed asynchronously",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202 Accepted
	json.NewEncoder(w).Encode(resp)

	log.Printf("✅ Order created: %s", orderID)
}

// HealthCheck handles GET /health
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// OrderHistoryResponse is the response for order history
type OrderHistoryResponse struct {
	OrderID       string          `json:"order_id"`
	UserID        string          `json:"user_id"`
	FromAmount    float64         `json:"from_amount"`
	FromCurrency  string          `json:"from_currency"`
	ToCurrency    string          `json:"to_currency"`
	ToAmount      float64         `json:"to_amount"`
	ExecutedPrice float64         `json:"executed_price"`
	OrderType     string          `json:"order_type"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Timeline      []TimelineEvent `json:"timeline"`
}

// TimelineEvent represents a single event in order history
type TimelineEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	Version     int                    `json:"version"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// GetOrderHistory handles GET /orders/{orderID}
func (h *OrderHandler) GetOrderHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract orderID from URL path
	// URL format: /orders/{orderID}
	path := strings.TrimPrefix(r.URL.Path, "/orders/")
	orderID := strings.TrimSpace(path)

	if orderID == "" {
		http.Error(w, "order_id is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Load all events for timeline (from EventStore - source of truth)
	events, err := h.eventStore.Load(ctx, orderID)
	if err != nil {
		log.Printf("Failed to load events: %v", err)
		http.Error(w, "Failed to load order history", http.StatusInternalServerError)
		return
	}

	if len(events) == 0 {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	// Extract order summary from events (aggregate state)
	var (
		userID        string
		fromAmount    float64
		fromCurrency  string
		toCurrency    string
		toAmount      float64
		executedPrice float64
		orderType     string
		status        string
		createdAt     time.Time
		updatedAt     time.Time
	)

	// Parse first event (OrderAccepted) for basic info
	var firstEvent map[string]interface{}
	if err := json.Unmarshal(events[0].EventData, &firstEvent); err == nil {
		userID, _ = firstEvent["user_id"].(string)
		fromAmount, _ = firstEvent["from_amount"].(float64)
		fromCurrency, _ = firstEvent["from_currency"].(string)
		toCurrency, _ = firstEvent["to_currency"].(string)
		orderType, _ = firstEvent["order_type"].(string)
		status = "pending"
		createdAt, _ = time.Parse(time.RFC3339, events[0].CreatedAt)
	}

	// Parse last event for latest state
	lastEvent := events[len(events)-1]
	updatedAt, _ = time.Parse(time.RFC3339, lastEvent.CreatedAt)

	// Update state based on event type
	for _, evt := range events {
		var eventData map[string]interface{}
		json.Unmarshal(evt.EventData, &eventData)

		switch evt.EventType {
		case "PriceQuoted":
			if p, ok := eventData["price"].(float64); ok {
				executedPrice = p
			}
			if ta, ok := eventData["to_amount"].(float64); ok {
				toAmount = ta
			}
		case "SwapExecuting":
			status = "executing"
		case "SwapExecuted":
			if ta, ok := eventData["to_amount"].(float64); ok {
				toAmount = ta
			}
			if p, ok := eventData["executed_price"].(float64); ok {
				executedPrice = p
			}
		case "OrderCompleted":
			status = "completed"
			if fa, ok := eventData["from_amount"].(float64); ok {
				fromAmount = fa
			}
			if ta, ok := eventData["to_amount"].(float64); ok {
				toAmount = ta
			}
			if p, ok := eventData["executed_price"].(float64); ok {
				executedPrice = p
			}
		case "OrderFailed":
			status = "failed"
		}
	}

	// Build timeline from events
	timeline := make([]TimelineEvent, 0, len(events))
	for _, evt := range events {
		// Parse timestamp from string
		timestamp, _ := time.Parse(time.RFC3339, evt.CreatedAt)

		timelineEvent := TimelineEvent{
			Timestamp: timestamp,
			EventType: evt.EventType,
			Version:   evt.Version,
		}

		// Parse event data for details
		var eventData map[string]interface{}
		if err := json.Unmarshal(evt.EventData, &eventData); err == nil {
			timelineEvent.Details = eventData
		}

		// Add human-readable description
		switch evt.EventType {
		case "OrderAccepted":
			timelineEvent.Description = "Order created and accepted for processing"
		case "PriceQuoted":
			if price, ok := eventData["price"].(float64); ok {
				if toAmount, ok := eventData["to_amount"].(float64); ok {
					timelineEvent.Description = fmt.Sprintf("Price quoted: %.2f per unit, receiving %.8f units", price, toAmount)
				}
			}
		case "SwapExecuting":
			timelineEvent.Description = "Swap execution started"
		case "SwapExecuted":
			if txHash, ok := eventData["transaction_hash"].(string); ok {
				timelineEvent.Description = "Swap executed successfully: " + txHash
			}
		case "OrderCompleted":
			timelineEvent.Description = "Order completed successfully"
		case "OrderFailed":
			if reason, ok := eventData["reason"].(string); ok {
				timelineEvent.Description = "Order failed: " + reason
			}
		case "PositionCreated":
			timelineEvent.Description = "Position created"
		case "PositionUpdated":
			timelineEvent.Description = "Position updated with order results"
		default:
			timelineEvent.Description = evt.EventType
		}

		timeline = append(timeline, timelineEvent)
	}

	// Build response (from events - source of truth)
	response := OrderHistoryResponse{
		OrderID:       orderID,
		UserID:        userID,
		FromAmount:    fromAmount,
		FromCurrency:  fromCurrency,
		ToCurrency:    toCurrency,
		ToAmount:      toAmount,
		ExecutedPrice: executedPrice,
		OrderType:     orderType,
		Status:        status,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Timeline:      timeline,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	log.Printf("📊 Order history retrieved: %s", orderID)
}
