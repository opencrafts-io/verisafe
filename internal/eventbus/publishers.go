package eventbus

import (
	"context"

	"github.com/opencrafts-io/verisafe/internal/repository"
)

// UserPublisher is the publish-side surface of UserEventBus that HTTP
// handlers depend on. Declared as a narrow interface, separately from the
// concrete type that dials RabbitMQ, so a handler test can substitute a mock
// instead of standing up a broker connection.
//
//go:generate go tool mockgen -source=publishers.go -destination=mocks/publishers.go -package=mockeventbus
type UserPublisher interface {
	PublishUserCreated(ctx context.Context, user repository.Account, requestID string) error
	PublishUserUpdated(ctx context.Context, user repository.Account, requestID string) error
	PublishUserDeleted(ctx context.Context, user repository.Account, requestID string) error
}

// InstitutionPublisher is the publish-side surface of InstitutionEventBus.
type InstitutionPublisher interface {
	PublishInstitutionCreated(ctx context.Context, institution repository.Institution, requestID string) error
	PublishInstitutionUpdated(ctx context.Context, institution repository.Institution, requestID string) error
	PublishInstitutionDeleted(ctx context.Context, institution repository.Institution, requestID string) error
}

// NotificationPublisher is the publish-side surface of NotificationEventBus.
type NotificationPublisher interface {
	PublishPushNotificationRequested(ctx context.Context, notification NotificationPayload, requestID string) error
}

// Close is deliberately excluded from all three interfaces above: the App
// owns each bus's lifecycle and closes it at shutdown, so a handler holding
// only the publish surface cannot accidentally close a bus other requests
// still depend on.
//
// The assertions below are what make this interface trustworthy going
// forward: without them, a signature change to any concrete method would
// drift silently from its interface instead of failing the build, which is
// exactly how broker.MessagePublisher rotted before it was deleted.
var (
	_ UserPublisher         = (*UserEventBus)(nil)
	_ InstitutionPublisher  = (*InstitutionEventBus)(nil)
	_ NotificationPublisher = (*NotificationEventBus)(nil)
)
