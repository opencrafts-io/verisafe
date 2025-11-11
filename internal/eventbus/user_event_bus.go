package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// UserEventBus provides a type-safe API for user events.
type UserEventBus struct {
	bus    EventBus
	logger *slog.Logger
}

// NewUserEventBus creates a new UserEventBus instance.
func NewUserEventBus(cfg *config.Config, logger *slog.Logger) (*UserEventBus, error) {
	rabbitMQConnString := fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.RabbitMQConfig.RabbitMQUser,
		cfg.RabbitMQConfig.RabbitMQPass,
		cfg.RabbitMQConfig.RabbitMQAddress,
		cfg.RabbitMQConfig.RabbitMQPort,
	)

	rabbitMQBus, err := NewRabbitMQEventBus(
		rabbitMQConnString,
		cfg.RabbitMQConfig.Exchange,
		FanoutExchangeType,
	)

	if err != nil {
		logger.Error("Failed to initialize RabbitMQ event bus", "error", err)
		return nil, fmt.Errorf("failed to initialize RabbitMQ event bus: %w", err)
	}

	return &UserEventBus{
		bus:    rabbitMQBus,
		logger: logger,
	}, nil
}

// PublishUserCreated publishes a user created event to the event bus
func (b *UserEventBus) PublishUserCreated(ctx context.Context, user repository.Account, requestID string) error {
	event := UserEvent{
		User: user,
		Metadata: UserEventMetadata{
			EventType:       "user.created",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "verisafe.user.created"
	b.logger.Info("Publishing user created event",
		slog.String("routing_key", routingKey),
		slog.String("user_id", user.ID.String()),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}

// PublishUserUpdated publishes a user updated event to the event bus
func (b *UserEventBus) PublishUserUpdated(ctx context.Context, user repository.Account, requestID string) error {
	event := UserEvent{
		User: user,
		Metadata: UserEventMetadata{
			EventType:       "user.updated",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "verisafe.user.updated"
	b.logger.Info("Publishing user updated event",
		slog.String("routing_key", routingKey),
		slog.String("user_id", user.ID.String()),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}

// PublishUserDeleted publishes a user deleted event to the event bus
func (b *UserEventBus) PublishUserDeleted(ctx context.Context, user repository.Account, requestID string) error {
	event := UserEvent{
		User: user,
		Metadata: UserEventMetadata{
			EventType:       "user.deleted",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "verisafe.user.deleted"
	b.logger.Info("Publishing user deleted event",
		slog.String("routing_key", routingKey),
		slog.String("user_id", user.ID.String()),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}

// GenerateRequestID generates a unique request ID for event tracking
func GenerateRequestID() string {
	return uuid.New().String()
}
