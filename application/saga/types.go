package saga

import (
	"context"
	"fmt"
)

// ===============================================
// Shared Types and Interfaces
// ===============================================

// PriceService интерфейс для получения цен
type PriceService interface {
	GetMarketPrice(ctx context.Context, from, to string) (float64, error)
}

// TradeWorker интерфейс для исполнения swap
type TradeWorker interface {
	ExecuteSwap(ctx context.Context, req SwapRequest) (*SwapResponse, error)
}

// SwapRequest represents a blockchain swap request
type SwapRequest struct {
	IdempotencyKey string
	FromCurrency   string
	ToCurrency     string
	FromAmount     float64
	Slippage       float64
}

// SwapResponse represents the result of a blockchain swap
type SwapResponse struct {
	TransactionHash string
	ToAmount        float64
	ExecutedPrice   float64
	Fees            float64
	Slippage        float64
}

// ===============================================
// Helper Functions
// ===============================================

// generateIdempotencyKey creates a unique key for swap operations
func generateIdempotencyKey(orderID string) string {
	return fmt.Sprintf("swap-%s", orderID)
}
