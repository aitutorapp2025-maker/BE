// Package queue wraps the RabbitMQ connection + channel used for publishing and
// consuming background jobs (e.g. homework processing, notifications).
//
// It AUTO-RECONNECTS: if the connection drops (network blip, broker restart), a
// background watcher redials with capped backoff and re-subscribes every
// registered consumer, so workers keep running without a manual app restart.
package queue

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// consumerReg remembers a Consume registration so it can be re-subscribed after
// a reconnect.
type consumerReg struct {
	queue   string
	handler func(body []byte) error
}

// RabbitMQ holds a managed connection + channel that auto-reconnect on failure.
type RabbitMQ struct {
	url string

	mu        sync.RWMutex
	conn      *amqp.Connection
	channel   *amqp.Channel
	consumers []consumerReg
	closed    bool
}

// Connect dials RabbitMQ, opens a channel, and starts the reconnect watcher.
func Connect(cfg config.Config) (*RabbitMQ, error) {
	r := &RabbitMQ{url: cfg.RabbitMQ.URL}
	if err := r.dial(); err != nil {
		return nil, err
	}
	go r.watch()
	return r, nil
}

// dial opens a fresh connection + channel and stores them under the lock.
func (r *RabbitMQ) dial() error {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq channel: %w", err)
	}
	r.mu.Lock()
	r.conn = conn
	r.channel = ch
	r.mu.Unlock()
	return nil
}

// watch blocks on the connection's close notification and reconnects whenever it
// fires (unless we closed intentionally).
func (r *RabbitMQ) watch() {
	for {
		r.mu.RLock()
		conn := r.conn
		closed := r.closed
		r.mu.RUnlock()
		if closed || conn == nil {
			return
		}
		// Block until THIS connection closes, then react.
		err := <-conn.NotifyClose(make(chan *amqp.Error, 1))

		r.mu.RLock()
		closed = r.closed
		r.mu.RUnlock()
		if closed {
			return // intentional shutdown — stop watching
		}
		log.Printf("[rabbitmq] connection lost (%v) — reconnecting...", err)
		r.reconnect()
	}
}

// reconnect redials with capped exponential backoff, then re-subscribes every
// registered consumer on the fresh channel.
func (r *RabbitMQ) reconnect() {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		r.mu.RLock()
		closed := r.closed
		r.mu.RUnlock()
		if closed {
			return
		}
		if err := r.dial(); err != nil {
			log.Printf("[rabbitmq] redial failed: %v — retrying in %s", err, backoff)
			time.Sleep(backoff)
			if backoff < maxBackoff {
				if backoff *= 2; backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		// Re-subscribe every consumer on the new channel.
		r.mu.RLock()
		regs := make([]consumerReg, len(r.consumers))
		copy(regs, r.consumers)
		r.mu.RUnlock()

		failed := false
		for _, c := range regs {
			if err := r.subscribe(c.queue, c.handler); err != nil {
				log.Printf("[rabbitmq] re-subscribe %q failed: %v — retrying", c.queue, err)
				failed = true
				break
			}
		}
		if failed {
			time.Sleep(backoff)
			continue
		}
		log.Printf("[rabbitmq] reconnected — %d consumer(s) restored", len(regs))
		return
	}
}

// Publish sends a message to the named queue (declared durable on first use).
func (r *RabbitMQ) Publish(queue string, body []byte) error {
	r.mu.RLock()
	ch := r.channel
	r.mu.RUnlock()
	if ch == nil {
		return fmt.Errorf("rabbitmq: not connected")
	}
	q, err := ch.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declare queue %q: %w", queue, err)
	}
	return ch.Publish("", q.Name, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

// Consume registers a handler for a durable queue and processes deliveries in a
// background goroutine (one at a time). The registration is remembered so it is
// AUTOMATICALLY re-subscribed after a reconnect. If the handler returns nil the
// message is acked; if it returns an error the message is dropped (nack, no
// requeue) so a poison message can't loop forever.
func (r *RabbitMQ) Consume(queue string, handler func(body []byte) error) error {
	r.mu.Lock()
	r.consumers = append(r.consumers, consumerReg{queue: queue, handler: handler})
	r.mu.Unlock()
	return r.subscribe(queue, handler)
}

// subscribe declares the queue and starts a delivery goroutine on the current
// channel. Used by Consume and re-run for each consumer after a reconnect.
func (r *RabbitMQ) subscribe(queue string, handler func(body []byte) error) error {
	r.mu.RLock()
	ch := r.channel
	r.mu.RUnlock()
	if ch == nil {
		return fmt.Errorf("rabbitmq: not connected")
	}
	q, err := ch.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declare queue %q: %w", queue, err)
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}
	deliveries, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %q: %w", queue, err)
	}

	go func() {
		// When the connection/channel drops, `deliveries` is closed and this
		// loop ends; the watcher then re-subscribes (starting a fresh goroutine).
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

// Healthy reports whether the connection is currently open (used by /health).
func (r *RabbitMQ) Healthy() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conn != nil && !r.conn.IsClosed()
}

// Close stops reconnecting and tears down the channel and connection.
func (r *RabbitMQ) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	ch := r.channel
	conn := r.conn
	r.mu.Unlock()
	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}
