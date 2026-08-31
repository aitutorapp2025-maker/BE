package wa

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueueWa is the RabbitMQ queue name for outgoing WhatsApp messages.
const QueueWa = "wa.send"

// Job is one WhatsApp message, published to RabbitMQ and delivered by the
// WhatsApp worker in the background (same pattern as email/SMS/push).
// Kind "otp" sends the Authentication template carrying Code; anything else
// sends Text as a normal message.
type Job struct {
	Phone string `json:"phone"`
	Text  string `json:"text"`
	Kind  string `json:"kind,omitempty"`
	Code  string `json:"code,omitempty"`
}

// Publisher enqueues WhatsApp jobs so callers (e.g. the daily-report cron)
// return immediately and delivery happens in the background worker.
type Publisher struct {
	mq      *queue.RabbitMQ
	enabled func() bool
}

// NewPublisher builds a Publisher. enabled is evaluated per-call so pasting the
// token in admin Settings takes effect without a restart.
func NewPublisher(mq *queue.RabbitMQ, enabled func() bool) *Publisher {
	return &Publisher{mq: mq, enabled: enabled}
}

// Enabled reports whether WhatsApp sending is currently configured.
func (p *Publisher) Enabled() bool { return p.enabled() }

// Enqueue publishes one WhatsApp message to the queue.
func (p *Publisher) Enqueue(job Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.mq.Publish(QueueWa, body)
}
