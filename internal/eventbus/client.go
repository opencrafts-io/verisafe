package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ExchangeType string

const (
	DirectExchangeType ExchangeType = "direct"
	FanoutExchangeType ExchangeType = "fanout"
	TopicExchangeType  ExchangeType = "topic"

	reconnectDelay = 5 * time.Second
)

// EventBus is an interface that defines the contract for any event bus implementation.
type EventBus interface {
	Publish(ctx context.Context, routingKey string, event any) error
	Subscribe(routingKey string, handler func(event []byte)) error
	Close()
}

// subscription holds the info needed to re-register a consumer after reconnect.
type subscription struct {
	routingKey string
	handler    func([]byte)
}

// RabbitMQEventBus is a concrete implementation of EventBus that uses RabbitMQ.
// It maintains a dedicated publish channel and creates a new channel per subscriber.
// It automatically reconnects on connection loss.
type RabbitMQEventBus struct {
	amqpURI      string
	exchange     string
	exchangeType ExchangeType
	logger       *slog.Logger

	mu        sync.RWMutex
	conn      *amqp.Connection
	publishCh *amqp.Channel // dedicated channel for publishing

	subscriptions []subscription // kept so we can re-subscribe on reconnect

	done chan struct{} // closed when Close() is called
}

// NewRabbitMQEventBus creates and returns a new RabbitMQEventBus instance.
// It connects to RabbitMQ and declares a durable exchange, then starts a
// background goroutine that reconnects automatically on connection loss.
func NewRabbitMQEventBus(amqpURI, exchange string, exchangeType ExchangeType, logger *slog.Logger) (*RabbitMQEventBus, error) {
	eb := &RabbitMQEventBus{
		amqpURI:      amqpURI,
		exchange:     exchange,
		exchangeType: exchangeType,
		logger:       logger,
		done:         make(chan struct{}),
	}

	if err := eb.connect(); err != nil {
		return nil, err
	}

	go eb.reconnectLoop()

	return eb, nil
}

// connect establishes the AMQP connection, declares the exchange, and opens
// a dedicated publish channel. It is called both on initial startup and after
// a connection drop is detected.
func (eb *RabbitMQEventBus) connect() error {
	conn, err := amqp.DialConfig(eb.amqpURI, amqp.Config{
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}

	publishCh, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("open publish channel: %w", err)
	}

	if err = publishCh.ExchangeDeclare(
		eb.exchange,
		string(eb.exchangeType),
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		conn.Close()
		return fmt.Errorf("declare exchange: %w", err)
	}

	eb.mu.Lock()
	eb.conn = conn
	eb.publishCh = publishCh
	eb.mu.Unlock()

	return nil
}

// reconnectLoop watches for connection-level close notifications and
// re-establishes the connection (and all subscriptions) automatically.
func (eb *RabbitMQEventBus) reconnectLoop() {
	for {
		eb.mu.RLock()
		conn := eb.conn
		eb.mu.RUnlock()

		connClose := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-eb.done:
			return
		case amqpErr, ok := <-connClose:
			if !ok {
				// channel closed without error — shutting down cleanly
				return
			}
			eb.logger.Warn("eventbus connection lost, reconnecting",
				slog.Any("error", amqpErr),
				slog.Duration("delay", reconnectDelay),
			)
		}

		// Back-off then reconnect.
		select {
		case <-eb.done:
			return
		case <-time.After(reconnectDelay):
		}

		for {
			if err := eb.connect(); err != nil {
				eb.logger.Error("eventbus reconnect failed, retrying",
					slog.Any("error", err),
					slog.Duration("delay", reconnectDelay),
				)
				select {
				case <-eb.done:
					return
				case <-time.After(reconnectDelay):
				}
				continue
			}
			eb.logger.Info("eventbus reconnected successfully")
			break
		}

		// Re-register all existing subscriptions on the new connection.
		eb.mu.RLock()
		subs := make([]subscription, len(eb.subscriptions))
		copy(subs, eb.subscriptions)
		eb.mu.RUnlock()

		for _, s := range subs {
			if err := eb.startConsumer(s.routingKey, s.handler); err != nil {
				eb.logger.Error("eventbus failed to re-subscribe after reconnect",
					slog.String("routing_key", s.routingKey),
					slog.Any("error", err),
				)
			}
		}
	}
}

// Publish serialises the event and sends it to the RabbitMQ exchange using
// the dedicated publish channel.
func (eb *RabbitMQEventBus) Publish(ctx context.Context, routingKey string, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	eb.mu.RLock()
	ch := eb.publishCh
	eb.mu.RUnlock()

	if ch == nil {
		return fmt.Errorf("eventbus: not connected")
	}

	return ch.PublishWithContext(
		ctx,
		eb.exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)
}

// Subscribe declares a durable queue, binds it to the exchange with the given
// routing key, and begins consuming messages in a background goroutine.
// Each subscriber gets its own AMQP channel (required by RabbitMQ).
func (eb *RabbitMQEventBus) Subscribe(routingKey string, handler func(event []byte)) error {
	eb.mu.Lock()
	eb.subscriptions = append(eb.subscriptions, subscription{routingKey, handler})
	eb.mu.Unlock()

	return eb.startConsumer(routingKey, handler)
}

// startConsumer opens a fresh channel and wires up a consumer for the given
// routing key. It also watches for channel-level close events and logs them
// (reconnection is handled at the connection level by reconnectLoop).
func (eb *RabbitMQEventBus) startConsumer(routingKey string, handler func([]byte)) error {
	eb.mu.RLock()
	conn := eb.conn
	eb.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("eventbus: not connected")
	}

	// Each consumer gets its own channel — RabbitMQ best practice.
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open consumer channel: %w", err)
	}

	queueName := fmt.Sprintf("io.opencrafts.gossip-monger.%s", routingKey)

	q, err := ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // do NOT auto-delete — keeps the queue alive between consumer restarts
		false, // not exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("declare queue %q: %w", queueName, err)
	}

	if err = ch.QueueBind(q.Name, routingKey, eb.exchange, false, nil); err != nil {
		ch.Close()
		return fmt.Errorf("bind queue %q: %w", q.Name, err)
	}

	msgs, err := ch.Consume(
		q.Name,
		"",    // consumer tag — let RabbitMQ generate one
		false, // auto-ack disabled — we ack manually after processing
		false, // not exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("consume queue %q: %w", q.Name, err)
	}

	go func() {
		chClose := ch.NotifyClose(make(chan *amqp.Error, 1))
		for {
			select {
			case d, ok := <-msgs:
				if !ok {
					// msgs channel closed — connection was lost; reconnectLoop will handle it.
					return
				}
				handler(d.Body)
				if err := d.Ack(false); err != nil {
					eb.logger.Error("eventbus ack failed",
						slog.String("routing_key", routingKey),
						slog.Any("error", err),
					)
				}

			case amqpErr, ok := <-chClose:
				if ok {
					eb.logger.Warn("eventbus consumer channel closed",
						slog.String("routing_key", routingKey),
						slog.Any("error", amqpErr),
					)
				}
				return

			case <-eb.done:
				ch.Close()
				return
			}
		}
	}()

	return nil
}

// Close gracefully shuts down the event bus, stopping all consumers and
// closing the AMQP connection.
func (eb *RabbitMQEventBus) Close() {
	close(eb.done)

	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.publishCh != nil {
		eb.publishCh.Close()
	}
	if eb.conn != nil {
		eb.conn.Close()
	}
}

