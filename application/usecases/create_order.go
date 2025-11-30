package usecases

import (
	"context"
	"fmt"

	"market_order/domain/order"
	"market_order/infrastructure/repository"
)

type CreateOrderUseCase struct {
	orderRepo *repository.OrderRepository
}

func NewCreateOrderUseCase(repo *repository.OrderRepository) *CreateOrderUseCase {
	return &CreateOrderUseCase{orderRepo: repo}
}

type CreateOrderRequest struct {
	OrderID      string
	UserID       string
	FromAmount   float64
	FromCurrency string
	ToCurrency   string
	OrderType    string
}

func (uc *CreateOrderUseCase) Execute(ctx context.Context, req CreateOrderRequest) error {
	// Создаём новый агрегат
	o := order.NewOrder()

	// Выполняем команду
	err := o.AcceptOrder(
		req.OrderID,
		req.UserID,
		req.FromAmount,
		req.FromCurrency,
		req.ToCurrency,
		req.OrderType,
	)
	if err != nil {
		return err
	}

	fmt.Println("event accept order: ", o)

	// Сохраняем события (Event Store + Outbox)
	return uc.orderRepo.Save(ctx, o)
}
