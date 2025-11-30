package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"market_order/domain/order"
	"market_order/infrastructure/idempotency"
	"market_order/infrastructure/messaging"
	"market_order/infrastructure/repository"
)

// NotificationService listens to domain events and sends notifications
type NotificationService struct {
	orderRepo       *repository.OrderRepository
	positionRepo    *repository.PositionRepository
	processedEvents *idempotency.ProcessedEventsRepository
	messageBus      *messaging.RabbitMQ
	notifier        Notifier
}

// Notifier interface for sending notifications (Telegram, Email, etc.)
type Notifier interface {
	SendMessage(ctx context.Context, userID, message string) error
}

func NewNotificationService(
	orderRepo *repository.OrderRepository,
	positionRepo *repository.PositionRepository,
	processedEvents *idempotency.ProcessedEventsRepository,
	messageBus *messaging.RabbitMQ,
	notifier Notifier,
) *NotificationService {
	return &NotificationService{
		orderRepo:       orderRepo,
		positionRepo:    positionRepo,
		processedEvents: processedEvents,
		messageBus:      messageBus,
		notifier:        notifier,
	}
}

// Start begins listening to events
func (ns *NotificationService) Start(ctx context.Context) error {
	// Subscribe to OrderCompleted events
	if err := ns.messageBus.Subscribe("OrderCompleted", ns.handleOrderCompleted); err != nil {
		return err
	}

	// Subscribe to OrderFailed events
	if err := ns.messageBus.Subscribe("OrderFailed", ns.handleOrderFailed); err != nil {
		return err
	}

	log.Println("✅ Notification Service started, listening for events...")

	<-ctx.Done()
	return nil
}

// handleOrderCompleted processes OrderCompleted events
func (ns *NotificationService) handleOrderCompleted(ctx context.Context, eventData []byte) error {
	log.Println("📨 NotificationService: Received OrderCompleted event")

	var evt order.OrderCompleted
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	processed, err := ns.processedEvents.IsProcessed(ctx, evt.EventID)
	if err != nil {
		return err
	}
	if processed {
		log.Printf("⏭️  Event %s already processed, skipping notification", evt.EventID)
		return nil
	}

	// Load order for details
	o, err := ns.orderRepo.Get(ctx, evt.AggregateID)
	if err != nil {
		log.Printf("⚠️  Failed to load order: %v", err)
		return err
	}

	// Format notification message
	message := fmt.Sprintf(
		"✅ Order Completed!\n\n"+
			"Order ID: %s\n"+
			"From: %.2f %s\n"+
			"To: %.8f %s\n"+
			"Price: %.2f %s/%s\n"+
			"Status: %s",
		o.ID,
		o.FromAmount, o.FromCurrency,
		o.ToAmount, o.ToCurrency,
		o.ExecutedPrice, o.FromCurrency, o.ToCurrency,
		o.Status,
	)

	// Send notification
	if err := ns.notifier.SendMessage(ctx, o.UserID, message); err != nil {
		log.Printf("⚠️  Failed to send notification: %v", err)
		return err
	}

	log.Printf("📤 Notification sent to user %s", o.UserID)

	// Mark as processed
	return ns.processedEvents.MarkAsProcessed(
		ctx,
		evt.EventID,
		evt.AggregateID,
		evt.EventType,
		"notification-service",
	)
}

// handleOrderFailed processes OrderFailed events
func (ns *NotificationService) handleOrderFailed(ctx context.Context, eventData []byte) error {
	log.Println("📨 NotificationService: Received OrderFailed event")

	var evt order.OrderFailed
	if err := json.Unmarshal(eventData, &evt); err != nil {
		return err
	}

	// Idempotency check
	processed, err := ns.processedEvents.IsProcessed(ctx, evt.EventID)
	if err != nil {
		return err
	}
	if processed {
		log.Printf("⏭️  Event %s already processed, skipping notification", evt.EventID)
		return nil
	}

	// Load order for details
	o, err := ns.orderRepo.Get(ctx, evt.AggregateID)
	if err != nil {
		log.Printf("⚠️  Failed to load order: %v", err)
		return err
	}

	// Format notification message
	message := fmt.Sprintf(
		"❌ Order Failed\n\n"+
			"Order ID: %s\n"+
			"Amount: %.2f %s\n"+
			"Reason: %s\n"+
			"Status: %s",
		o.ID,
		o.FromAmount, o.FromCurrency,
		evt.Reason,
		o.Status,
	)

	// Send notification
	if err := ns.notifier.SendMessage(ctx, o.UserID, message); err != nil {
		log.Printf("⚠️  Failed to send notification: %v", err)
		return err
	}

	log.Printf("📤 Failure notification sent to user %s", o.UserID)

	// Mark as processed
	return ns.processedEvents.MarkAsProcessed(
		ctx,
		evt.EventID,
		evt.AggregateID,
		evt.EventType,
		"notification-service",
	)
}

// MockNotifier is a simple console notifier for testing
type MockNotifier struct{}

func (m *MockNotifier) SendMessage(ctx context.Context, userID, message string) error {
	log.Printf("📱 [MOCK NOTIFICATION] To: %s\n%s\n", userID, message)
	return nil
}
