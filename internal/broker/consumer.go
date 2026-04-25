package broker

import (
	"context"
	"fmt"
	"log/slog"
)

type ExchangeType string

const (
	DirectExchangeType ExchangeType = "direct"
	FanoutExchangeType ExchangeType = "fanout"
	TopicExchangeType  ExchangeType = "topic"
)

// MessageHandler is the callback function that processes consumed messages
type MessageHandler func(ctx context.Context, message []byte) error

// MessageConsumer defines the contract for consuming messages from RabbitMQ
type MessageConsumer interface {
	Consume(
		ctx context.Context,
		exchange string,
		exchangeType ExchangeType,
		queue string,
		bindingKey string,
		handler MessageHandler,
	) error
}

// Consumer implements MessageConsumer
type Consumer struct {
	conn          Connection
	prefetchCount int
	logger        slog.Logger
}

// NewConsumer creates a new Consumer instance with configurable prefetch count
// prefetchCount limits the number of unacknowledged messages delivered to this consumer
// Recommended: 1 for serial processing, higher for parallel processing
func NewConsumer(
	conn Connection,
	prefetchCount int,
	logger slog.Logger,
) *Consumer {
	return &Consumer{
		conn:          conn,
		prefetchCount: prefetchCount,
		logger:        logger,
	}
}

// Consume starts consuming messages from a queue and passes them to the handler
func (c *Consumer) Consume(
	ctx context.Context,
	exchange string,
	exchangeType ExchangeType,
	queue string,
	bindingKey string,
	handler MessageHandler,
) error {
	ch := c.conn.Channel()
	if ch == nil {
		err := fmt.Errorf("channel is nil")
		c.logger.Error("cannot consume messages", "error", err)
		return err
	}
	defer ch.Close()
	err := ch.ExchangeDeclare(
		exchange,             // name
		string(exchangeType), // type
		true,                 // durable (survives restart)
		false,                // auto-deleted
		false,                // internal
		false,                // no-wait
		nil,                  // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	_, err = ch.QueueDeclare(
		queue, // name
		true,  // durable (recommended)
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	err = ch.QueueBind(
		queue,      // queue name
		bindingKey, // routing pattern (e.g., "gossip.#")
		exchange,   // exchange name
		false,      // no-wait
		nil,        // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	err = ch.Qos(
		c.prefetchCount, // prefetch count
		0,               // prefetch size (0 = no size limit)
		false,           // global (false = apply only to this consumer)
	)
	if err != nil {
		c.logger.Error("failed to set QoS",
			"queue", queue,
			"prefetch_count", c.prefetchCount,
			"error", err,
		)
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// Start consuming messages
	msgs, err := ch.Consume(
		queue, // queue name
		"",    // consumer tag (auto-generated)
		false, // auto-acknowledge
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		c.logger.Error("failed to start consuming",
			"queue", queue,
			"error", err,
		)
		return fmt.Errorf("failed to consume from queue: %w", err)
	}

	c.logger.Info("consumer started", "queue", queue)

	// Listen for messages
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("consumer stopped", "queue", queue)
			return ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				c.logger.Error("message channel closed", "queue", queue)
				return fmt.Errorf("message channel closed for queue: %s", queue)
			}

			// Call the handler with the message
			if err := handler(ctx, msg.Body); err != nil {
				c.logger.Error("handler error",
					"queue", queue,
					"error", err,
				)
				msg.Nack(false, false)
				// Continue consuming, don't stop on handler error
			} else {
				msg.Ack(false)
			}
		}
	}
}
