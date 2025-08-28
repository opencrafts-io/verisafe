package eventbus

import (
	"time"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

// UserEventMetadata contains crucial information about the event itself.
type UserEventMetadata struct {
	EventType       string    `json:"event_type"`
	Timestamp       time.Time `json:"timestamp"`
	SourceServiceID string    `json:"source_service_id"`
	RequestID       string    `json:"request_id"`
}

// UserEvent defines the payload for user-related events.
type UserEvent struct {
	User     repository.Account `json:"user"`
	Metadata UserEventMetadata  `json:"meta"`
}
