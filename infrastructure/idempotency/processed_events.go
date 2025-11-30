package idempotency

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// ProcessedEventsRepository manages idempotency checks for event processing
type ProcessedEventsRepository struct {
	db *sql.DB
}

func NewProcessedEventsRepository(db *sql.DB) *ProcessedEventsRepository {
	return &ProcessedEventsRepository{db: db}
}

// IsProcessed checks if an event has already been processed
func (r *ProcessedEventsRepository) IsProcessed(ctx context.Context, eventID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check processed event: %w", err)
	}

	return exists, nil
}

// MarkAsProcessed marks an event as processed (idempotency key)
func (r *ProcessedEventsRepository) MarkAsProcessed(
	ctx context.Context,
	eventID, aggregateID, eventType, processedBy string,
) error {
	query := `
		INSERT INTO processed_events (event_id, aggregate_id, event_type, processed_by, processed_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (event_id) DO NOTHING
	`

	_, err := r.db.ExecContext(ctx, query, eventID, aggregateID, eventType, processedBy)
	if err != nil {
		return fmt.Errorf("failed to mark event as processed: %w", err)
	}

	log.Printf("✅ Marked event %s as processed by %s", eventID, processedBy)
	return nil
}

// GetProcessedEvents returns all processed events for an aggregate (audit/debug)
func (r *ProcessedEventsRepository) GetProcessedEvents(
	ctx context.Context,
	aggregateID string,
) ([]ProcessedEvent, error) {
	query := `
		SELECT event_id, aggregate_id, event_type, processed_by, processed_at
		FROM processed_events
		WHERE aggregate_id = $1
		ORDER BY processed_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("failed to query processed events: %w", err)
	}
	defer rows.Close()

	var events []ProcessedEvent
	for rows.Next() {
		var e ProcessedEvent
		err := rows.Scan(&e.EventID, &e.AggregateID, &e.EventType, &e.ProcessedBy, &e.ProcessedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan processed event: %w", err)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// ProcessedEvent represents a processed event record
type ProcessedEvent struct {
	EventID     string
	AggregateID string
	EventType   string
	ProcessedBy string
	ProcessedAt string
}
