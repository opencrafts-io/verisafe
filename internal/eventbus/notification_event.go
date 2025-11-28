package eventbus

import (
	"time"
)

// NotificationEvent represents the complete notification event structure
type NotificationEvent struct {
	Notification NotificationPayload `json:"notification"`
	Meta         NotificationEventMetadata       `json:"meta"`
}

// NotificationPayload contains the notification details
type NotificationPayload struct {
	AppID                  string               `json:"app_id"`
	Headings               LocalizedText        `json:"headings"`
	Contents               LocalizedText        `json:"contents"`
	TargetUserID           string               `json:"target_user_id"`
	IncludeExternalUserIds []string             `json:"include_external_user_ids"`
	Subtitle               LocalizedText        `json:"subtitle"`
	AndroidChannelID       string               `json:"android_channel_id"`
	IosSound               string               `json:"ios_sound"`
	BigPicture             string               `json:"big_picture"`
	LargeIcon              string               `json:"large_icon"`
	SmallIcon              string               `json:"small_icon"`
	URL                    string               `json:"url"`
	Buttons                []NotificationButton `json:"buttons"`
}

// LocalizedText represents text in different languages
type LocalizedText map[string]string

// NotificationButton represents an action button in the notification
type NotificationButton struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Icon string `json:"icon"`
}

// EventMetadata contains metadata about the event
type NotificationEventMetadata struct {
	EventType       string    `json:"event_type"`
	SourceServiceID string    `json:"source_service_id"`
	RequestID       string    `json:"request_id"`
	Timestamp       time.Time `json:"timestamp,omitempty"` // Optional: add if you track when event was created
}
