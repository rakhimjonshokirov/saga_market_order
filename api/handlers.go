package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"market_order/application/usecases"
	pkguuid "market_order/pkg/uuid"
)

// OrderHandler handles HTTP requests for orders
type OrderHandler struct {
	createOrderUC *usecases.CreateOrderUseCase
}

func NewOrderHandler(createOrderUC *usecases.CreateOrderUseCase) *OrderHandler {
	return &OrderHandler{
		createOrderUC: createOrderUC,
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
