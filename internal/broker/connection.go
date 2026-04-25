package broker

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Connection interface {
	Channel() *amqp.Channel
	Close() error
	IsClosed() bool
}

type RabbitMQConnection struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitMQConnection(
	ctx context.Context,
	dsn string,
) (*RabbitMQConnection, error) {
	conn, err := amqp.Dial(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}

	return &RabbitMQConnection{
		conn:    conn,
		channel: ch,
	}, nil
}

// Channel returns the underlying AMQP channel
func (rc *RabbitMQConnection) Channel() *amqp.Channel {
	return rc.channel
}

// Close closes the connection and channel
func (rc *RabbitMQConnection) Close() error {
	if rc.channel != nil {
		rc.channel.Close()
	}
	if rc.conn != nil {
		return rc.conn.Close()
	}
	return nil
}

// IsClosed checks if the connection is closed
func (rc *RabbitMQConnection) IsClosed() bool {
	return rc.conn == nil || rc.conn.IsClosed()
}
