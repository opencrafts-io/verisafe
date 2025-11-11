// Copyright (c) 2025 Open Crafts Interactive Limited. All Rights Reserved.
// Author: erick.muuo@opencrafts.io
// 
// Documentation for the user eventbus
// 
// OVERVIEW:
// The UserEventBus provides a publish-subscribe event streaming system for user account lifecycle events.
// It leverages RabbitMQ as the underlying message broker with a fanout exchange pattern to distribute
// user events across multiple independent consumers in a loosely coupled architecture.
// 
// EXCHANGE TYPE: Fanout
// The user events are published to a RabbitMQ fanout exchange. In this pattern, all messages published
// to the exchange are broadcast to every queue that has been bound to that exchange, regardless of
// routing keys. The routing key is ignored in fanout exchanges, ensuring all subscribers receive
// every published event.
// 
// CLIENT QUEUE MANAGEMENT:
// Consuming services (clients) are responsible for creating and managing their own dedicated queues
// that bind to the user events exchange. Once a queue is bound to the exchange, it will automatically
// receive copies of all user events published by this service. This enables true publish-subscribe
// semantics where multiple independent services can subscribe to user events without requiring
// the publisher to know about or manage subscriber connections.
// 
// EVENT TYPES:
// The UserEventBus publishes three primary user lifecycle events:
// - user.created: Published when a new user account is created
// - user.updated: Published when an existing user account is modified
// - user.deleted: Published when a user account is deleted
// 
// Each event contains the complete user account information and metadata including timestamp,
// source service identifier, and a request ID for distributed tracing and correlation.
// 
// MESSAGE DELIVERY:
// Messages are delivered asynchronously to all bound queues. Each consuming service receives
// its own independent copy of each event message. There is no competition between consumers;
// if multiple services bind queues to this exchange, each will receive complete copies of all events.
// This architecture supports multiple independent processors acting on user events simultaneously.

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

	routingKey := ""
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

	routingKey := ""
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

	routingKey := ""
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
