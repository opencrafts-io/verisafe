package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ExchangeType string

const (
	DirectExchangeType ExchangeType = "direct"
	FanoutExchangeType ExchangeType = "fanout"
	TopicExchangeType  ExchangeType = "topic"
)

// EventBus is an interface that defines the contract for any event bus implementation.
// The Publish method accepts a routing key.
type EventBus interface {
	Publish(ctx context.Context, routingKey string, event any) error
	Subscribe(routingKey string, handler func(event []byte)) error
	Close()
}

// RabbitMQEventBus is a concrete implementation of EventBus that uses RabbitMQ.
type RabbitMQEventBus struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
}

// NewRabbitMQEventBus creates and returns a new RabbitMQEventBus instance.
// It connects to the RabbitMQ server and declares a durable exchange.
func NewRabbitMQEventBus(amqpURI, exchange string, exchangeType ExchangeType) (*RabbitMQEventBus, error) {
	conn, err := amqp.Dial(amqpURI)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Declare a durable direct exchange
	err = ch.ExchangeDeclare(
		exchange,             // name
		string(exchangeType), // type
		true,                 // durable
		false,                // auto-deleted
		false,                // internal
		false,                // no-wait
		nil,                  // arguments
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	return &RabbitMQEventBus{
		conn:     conn,
		channel:  ch,
		exchange: exchange,
	}, nil
}

// Publish serializes the event and sends it to the RabbitMQ exchange.
func (eb *RabbitMQEventBus) Publish(ctx context.Context, routingKey string, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	publishing := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent, // Make message persistent
	}

	return eb.channel.PublishWithContext(
		ctx,
		eb.exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		publishing,
	)
}

// Close closes the RabbitMQ channel and connection.
func (eb *RabbitMQEventBus) Close() {
	if eb.channel != nil {
		eb.channel.Close()
	}
	if eb.conn != nil {
		eb.conn.Close()
	}
}

func (eb *RabbitMQEventBus) Subscribe(routingKey string, handler func(event []byte)) error {
	return errors.New("Subscribe not implemented")
}
