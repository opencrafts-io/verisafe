package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/opencrafts-io/verisafe/internal/config"
)

type NotificationEventBus struct {
	bus    EventBus
	logger *slog.Logger
}

// NewUserEventBus creates a new UserEventBus instance.
func NewNotificationEventBus(cfg *config.Config, logger *slog.Logger) (*NotificationEventBus, error) {
	rabbitMQConnString := fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.RabbitMQConfig.RabbitMQUser,
		cfg.RabbitMQConfig.RabbitMQPass,
		cfg.RabbitMQConfig.RabbitMQAddress,
		cfg.RabbitMQConfig.RabbitMQPort,
	)

	rabbitMQBus, err := NewRabbitMQEventBus(
		rabbitMQConnString,
		"gossip-monger.exchange",
		DirectExchangeType,
	)

	if err != nil {
		logger.Error("Failed to initialize RabbitMQ event bus", "error", err)
		return nil, fmt.Errorf("failed to initialize RabbitMQ event bus: %w", err)
	}

	return &NotificationEventBus{
		bus:    rabbitMQBus,
		logger: logger,
	}, nil
}

// PublishPushNotification publishes an event to request a push
// notification to be sent to a user via gossip-monger
func (neb *NotificationEventBus) PublishPushNotificationRequested(
	ctx context.Context,
	notification NotificationPayload, requestID string,
) error {
	event := NotificationEvent{
		Notification: notification,
		Meta: NotificationEventMetadata{
			EventType:       "notification.requested",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "gossip-monger.notification.requested"
	neb.logger.Info("Publishing push notification requested event",
		slog.String("routing_key", routingKey),
		slog.String("request_id", requestID),
	)

	return neb.bus.Publish(ctx, routingKey, event)
}
