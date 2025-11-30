package uuid

import (
	"github.com/google/uuid"
)

// New generates a new UUID v4
func New() string {
	return uuid.New().String()
}

// NewUUID is an alias for New
func NewUUID() string {
	return New()
}

// Parse parses a UUID string
func Parse(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// MustParse parses a UUID string and panics on error
func MustParse(s string) uuid.UUID {
	return uuid.MustParse(s)
}
