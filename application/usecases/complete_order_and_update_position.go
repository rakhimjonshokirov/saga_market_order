package usecases

import (
	"context"
	"fmt"

	"market_order/infrastructure/eventstore"
	"market_order/infrastructure/repository"
)

type CompleteOrderAndUpdatePositionUseCase struct {
	orderRepo    *repository.OrderRepository
	positionRepo *repository.PositionRepository
	eventStore   eventstore.EventStore
}

func NewCompleteOrderAndUpdatePositionUseCase(
	orderRepo *repository.OrderRepository,
	positionRepo *repository.PositionRepository,
	eventStore eventstore.EventStore,
) *CompleteOrderAndUpdatePositionUseCase {
	return &CompleteOrderAndUpdatePositionUseCase{
		orderRepo:    orderRepo,
		positionRepo: positionRepo,
		eventStore:   eventStore,
	}
}

type SwapResult struct {
	TransactionHash string
	FromAmount      float64
	ToAmount        float64
	ExecutedPrice   float64
	Fees            float64
	Slippage        float64
}

// Execute completes order and updates position in a SINGLE TRANSACTION
// This is CRITICAL for consistency - both aggregates must be updated atomically
func (uc *CompleteOrderAndUpdatePositionUseCase) Execute(
	ctx context.Context,
	orderID, positionID string,
	swapResult SwapResult,
) error {
	// === 1. Загружаем Order ===
	o, err := uc.orderRepo.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to load order: %w", err)
	}

	// === 2. Завершаем Order ===
	if err := o.CompleteOrder(); err != nil {
		return fmt.Errorf("failed to complete order: %w", err)
	}

	// === 3. Загружаем Position ===
	p, err := uc.positionRepo.Get(ctx, positionID)
	if err != nil {
		return fmt.Errorf("failed to load position: %w", err)
	}

	// === 4. Обновляем Position ===
	totalValue := swapResult.FromAmount // Примерный расчёт
	pnl := 0.0                          // Для первого ордера

	if err := p.AddOrder(orderID, swapResult.ToAmount, totalValue, pnl); err != nil {
		return fmt.Errorf("failed to update position: %w", err)
	}

	// === 5. CRITICAL: Сохраняем ОБА агрегата в ОДНОЙ транзакции ===
	// Собираем все события из обоих агрегатов
	allEvents := make([]interface{}, 0)
	allEvents = append(allEvents, o.Changes...)
	allEvents = append(allEvents, p.Changes...)

	// Сохраняем все события атомарно
	// Event Store гарантирует, что либо ВСЕ события сохранятся, либо НИЧЕГО
	if err := uc.eventStore.Save(ctx, allEvents); err != nil {
		return fmt.Errorf("failed to save events: %w", err)
	}

	// Очищаем Changes после успешного сохранения
	o.Changes = nil
	p.Changes = nil

	return nil
}
