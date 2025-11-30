package outbox

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/lib/pq"
	"market_order/infrastructure/messaging"
)

// OutboxPublisher читает непубликованные события из outbox и публикует в RabbitMQ
type OutboxPublisher struct {
	db         *sql.DB
	messageBus *messaging.RabbitMQ
	interval   time.Duration
}

func NewOutboxPublisher(db *sql.DB, mb *messaging.RabbitMQ) *OutboxPublisher {
	return &OutboxPublisher{
		db:         db,
		messageBus: mb,
		interval:   100 * time.Millisecond,
	}
}

// Start запускает worker для публикации событий
func (op *OutboxPublisher) Start(ctx context.Context) error {
	ticker := time.NewTicker(op.interval)
	defer ticker.Stop()

	log.Println("Outbox Publisher started")

	for {
		select {
		case <-ticker.C:
			if err := op.publishPendingEvents(ctx); err != nil {
				log.Printf("Failed to publish events: %v", err)
			}

		case <-ctx.Done():
			log.Println("Outbox Publisher stopped")
			return nil
		}
	}
}

func (op *OutboxPublisher) publishPendingEvents(ctx context.Context) error {
	// Загружаем непубликованные события
	query := `
        SELECT id, event_id, aggregate_id, event_type, event_data
        FROM outbox
        WHERE published = false
        ORDER BY created_at ASC
        LIMIT 100
    `

	rows, err := op.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var publishedIDs []int64

	for rows.Next() {
		var (
			id          int64
			eventID     string
			aggregateID string
			eventType   string
			eventData   []byte
		)

		if err := rows.Scan(&id, &eventID, &aggregateID, &eventType, &eventData); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}

		// Публикуем в RabbitMQ
		if err := op.messageBus.Publish(eventType, eventData); err != nil {
			log.Printf("Failed to publish event %s: %v", eventID, err)
			continue
		}

		publishedIDs = append(publishedIDs, id)
	}

	// Помечаем как опубликованные
	if len(publishedIDs) > 0 {
		if err := op.markAsPublished(ctx, publishedIDs); err != nil {
			return err
		}

		log.Printf("Published %d events", len(publishedIDs))
	}

	return nil
}

func (op *OutboxPublisher) markAsPublished(ctx context.Context, ids []int64) error {
	query := `
        UPDATE outbox
        SET published = true, published_at = NOW()
        WHERE id = ANY($1)
    `

	// Use pq.Array for PostgreSQL array parameter
	_, err := op.db.ExecContext(ctx, query, pq.Array(ids))
	return err
}
