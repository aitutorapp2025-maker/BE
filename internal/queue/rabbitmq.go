// Package queue wraps the RabbitMQ connection + channel used for publishing and
// consuming background jobs (e.g. homework processing, notifications).
package queue

import (
	"fmt"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ holds an open connection and channel.
type RabbitMQ struct {
	Conn    *amqp.Connection
	Channel *amqp.Channel
}

// Connect dials RabbitMQ and opens a channel.
func Connect(cfg config.Config) (*RabbitMQ, error) {
	conn, err := amqp.Dial(cfg.RabbitMQ.URL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq channel: %w", err)
	}

	return &RabbitMQ{Conn: conn, Channel: ch}, nil
}

// Publish sends a message to the named queue (declared durable on first use).
func (r *RabbitMQ) Publish(queue string, body []byte) error {
	q, err := r.Channel.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declare queue %q: %w", queue, err)
	}
	return r.Channel.Publish("", q.Name, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

// Consume registers a handler for a durable queue and processes deliveries in a
// background goroutine (one at a time). If the handler returns nil the message
// is acked; if it returns an error the message is dropped (nack, no requeue) so
// a poison message can't loop forever.
func (r *RabbitMQ) Consume(queue string, handler func(body []byte) error) error {
	q, err := r.Channel.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declare queue %q: %w", queue, err)
	}
	if err := r.Channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}
	deliveries, err := r.Channel.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %q: %w", queue, err)
	}

	go func() {
		for d := range deliveries {
			if err := handler(d.Body); err != nil {
				_ = d.Nack(false, false)
			} else {
				_ = d.Ack(false)
			}
		}
	}()
	return nil
}

// Close tears down the channel and connection.
func (r *RabbitMQ) Close() {
	if r == nil {
		return
	}
	if r.Channel != nil {
		_ = r.Channel.Close()
	}
	if r.Conn != nil {
		_ = r.Conn.Close()
	}
}
