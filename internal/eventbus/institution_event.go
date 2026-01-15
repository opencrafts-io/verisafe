package eventbus

import (
	"time"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

type InstitutionEventMetaData struct {
	EventType       string    `json:"event_type"`
	Timestamp       time.Time `json:"timestamp"`
	SourceServiceID string    `json:"source_service_id"`
	RequestID       string    `json:"request_id"`
}

type InstitutionEvent struct {
	Institution repository.Institution   `json:"institution"`
	Metadata    InstitutionEventMetaData `json:"meta"`
}
