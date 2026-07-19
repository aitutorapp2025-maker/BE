package sms

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueueSend is the RabbitMQ queue name for outgoing SMS jobs.
const QueueSend = "sms.send"

// Job is a single outgoing SMS, published to the queue and consumed by the
// SMS worker.
type Job struct {
	To   string `json:"to"`
	Text string `json:"text"`
	// Force sends even when the "enabled" toggle is off (used by the admin
	// "test SMS" to verify credentials before enabling).
	Force bool `json:"force,omitempty"`
}

// Publisher enqueues SMS jobs onto RabbitMQ, so the HTTP request returns
// immediately and delivery is handled by a worker.
type Publisher struct {
	mq      *queue.RabbitMQ
	enabled func() bool
}

// NewPublisher builds a Publisher. enabled is evaluated per-call.
func NewPublisher(mq *queue.RabbitMQ, enabled func() bool) *Publisher {
	return &Publisher{mq: mq, enabled: enabled}
}

// Enabled reports whether SMS is currently configured.
func (p *Publisher) Enabled() bool { return p.enabled() }

// Enqueue publishes an SMS job to the queue.
func (p *Publisher) Enqueue(job Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.mq.Publish(QueueSend, body)
}
