package email

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueueSend is the RabbitMQ queue name for outgoing email jobs.
const QueueSend = "email.send"

// Job is a single outgoing email, published to the queue and consumed by the
// email worker.
type Job struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	// NoAlert is set on alert emails themselves so a failed alert delivery
	// doesn't trigger another alert (which would loop).
	NoAlert bool `json:"no_alert,omitempty"`
	// Force sends using the SMTP host even when the "enabled" toggle is off
	// (used by the admin "test email" to verify credentials before enabling).
	Force bool `json:"force,omitempty"`
}

// Publisher enqueues email jobs onto RabbitMQ instead of sending inline, so the
// HTTP request returns immediately and delivery is handled by a worker.
type Publisher struct {
	mq      *queue.RabbitMQ
	enabled func() bool
}

// NewPublisher builds a Publisher. [enabled] is evaluated per-call and reflects
// whether SMTP is currently configured — when false, callers skip enqueuing.
func NewPublisher(mq *queue.RabbitMQ, enabled func() bool) *Publisher {
	return &Publisher{mq: mq, enabled: enabled}
}

// Enabled reports whether outgoing email is currently configured.
func (p *Publisher) Enabled() bool { return p.enabled() }

// Enqueue publishes an email job to the queue.
func (p *Publisher) Enqueue(job Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.mq.Publish(QueueSend, body)
}
