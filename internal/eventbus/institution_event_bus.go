// Copyright (c) 2025 Open Crafts Interactive Limited. All Rights Reserved.
// Author: erick.muuo@opencrafts.io
//
// Documentation for the institution eventbus
//
// OVERVIEW:
// The InstitutionBus provides a publish-subscribe event streaming system for institution events.
// It leverages RabbitMQ as the underlying message broker with a direct exchange pattern to distribute
// institution events across multiple independent consumers in a loosely coupled architecture.
//
// EXCHANGE TYPE: Direct
// The institution events are published to a RabbitMQ direct exchange. The direct exchange routes
// messages to queues based on an exact match between the message's routing key and the binding
// key of the queue. This allows for targeted message delivery, making it useful for scenarios
// where specific routing is needed
//
// EVENT TYPES:
// The UserEventBus publishes three primary user lifecycle events:
// - institution.created: Published when an institution is created
// - institution.updated: Published when an institution is modified
// - institution.deleted: Published when an institution is deleted
//
// Each event contains the complete institution information and metadata including timestamp,
// source service identifier, and a request ID for distributed tracing and correlation.
//
// MESSAGE DELIVERY:
// Since its assummed at the moment that there is only one consumer to this kind of information,
// therefore information is delivered in the default round robin mechanism if there are multiple same
// services listening on the same queue

package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type InstitutionEventBus struct {
	bus    EventBus
	logger *slog.Logger
}

// NewInstitutionEventBus creates a new UserEventBus instance.
func NewInstitutionEventBus(cfg *config.Config, logger *slog.Logger) (*InstitutionEventBus, error) {
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

	return &InstitutionEventBus{
		bus:    rabbitMQBus,
		logger: logger,
	}, nil
}

// PublishInstitutionCreated publishes an institution created event to the event bus
func (b *InstitutionEventBus) PublishInstitutionCreated(ctx context.Context, institution repository.Institution, requestID string) error {
	event := InstitutionEvent{
		Institution: institution,
		Metadata: InstitutionEventMetaData{
			EventType:       "institution.created",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "institution.events"
	b.logger.Info("Publishing institution created event",
		slog.String("routing_key", routingKey),
		slog.Any("institution_id", institution.InstitutionID),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}

// PublishInstitutionUpdated publishes an institution updated event to the event bus
func (b *InstitutionEventBus) PublishInstitutionUpdated(ctx context.Context, institution repository.Institution, requestID string) error {
	event := InstitutionEvent{
		Institution: institution,
		Metadata: InstitutionEventMetaData{
			EventType:       "institution.updated",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "institution.events"
	b.logger.Info("Publishing institution updated event",
		slog.String("routing_key", routingKey),
		slog.Any("institution_id", institution.InstitutionID),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}

// PublishInstitutionDeleted publishes an institution deleted event to the event bus
func (b *InstitutionEventBus) PublishInstitutionDeleted(ctx context.Context, institution repository.Institution, requestID string) error {
	event := InstitutionEvent{
		Institution: institution,
		Metadata: InstitutionEventMetaData{
			EventType:       "institution.deleted",
			Timestamp:       time.Now(),
			SourceServiceID: "io.opencrafts.verisafe",
			RequestID:       requestID,
		},
	}

	routingKey := "institution.events"
	b.logger.Info("Publishing institution deleted event",
		slog.String("routing_key", routingKey),
		slog.Any("institution_id", institution.InstitutionID),
		slog.String("request_id", requestID),
	)

	return b.bus.Publish(ctx, routingKey, event)
}
