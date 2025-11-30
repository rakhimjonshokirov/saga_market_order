package eventstore

import (
	"encoding/json"
	"errors"
	"time"
)

// BaseFieldsProvider is an interface for events that can provide base fields
type BaseFieldsProvider interface {
	GetBaseEvent() BaseFields
}

// BaseFields contains common event fields
type BaseFields struct {
	EventID       string
	AggregateID   string
	AggregateType string
	EventType     string
	Version       int
	Timestamp     time.Time
}

// serializeEvent serializes an event and extracts base fields
func serializeEvent(event interface{}) ([]byte, []byte, BaseFields, error) {
	// Serialize entire event to JSON
	eventData, err := json.Marshal(event)
	if err != nil {
		return nil, nil, BaseFields{}, err
	}

	// Extract base fields
	provider, ok := event.(BaseFieldsProvider)
	if !ok {
		return nil, nil, BaseFields{}, errors.New("event must implement BaseFieldsProvider interface")
	}

	baseFields := provider.GetBaseEvent()

	// Metadata (empty for now, can be extended)
	metadata := []byte("{}")

	return eventData, metadata, baseFields, nil
}

// isUniqueViolation checks if error is a PostgreSQL unique constraint violation
func isUniqueViolation(err error) bool {
	// Check for PostgreSQL error code 23505 (unique_violation)
	// In production, use pgconn.PgError or pq.Error for proper detection
	if err == nil {
		return false
	}
	
	errMsg := err.Error()
	return errMsg != "" && (
		// PostgreSQL error patterns
		containsString(errMsg, "duplicate key value") ||
		containsString(errMsg, "unique constraint") ||
		containsString(errMsg, "23505"))
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}()))
}
