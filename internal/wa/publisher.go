package wa

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueueWa is the RabbitMQ queue name for outgoing WhatsApp messages.
const QueueWa = "wa.send"

// QueueWaInbox carries raw Meta webhook payloads (incoming customer
// messages) from the HTTP handler to the inbox worker.
const QueueWaInbox = "wa.inbox"

// Job is one WhatsApp message, published to RabbitMQ and delivered by the
// WhatsApp worker in the background (same pattern as email/SMS/push).
//   - Kind "otp"      → Authentication template carrying Code
//   - Kind "template" → a broadcast/campaign template send (Template + Lang +
//     Params + optional HeaderImageID); CampaignID/RecipientID let the worker
//     update per-recipient delivery status.
//   - anything else   → free-form Text
type Job struct {
	Phone string `json:"phone"`
	Text  string `json:"text"`
	Kind  string `json:"kind,omitempty"`
	Code  string `json:"code,omitempty"`
	// Template (campaign) fields:
	Template      string   `json:"template,omitempty"`
	Lang          string   `json:"lang,omitempty"`
	Params        []string `json:"params,omitempty"`
	HeaderImageID string   `json:"header_image_id,omitempty"`
	CampaignID    uint     `json:"campaign_id,omitempty"`
	RecipientID   uint     `json:"recipient_id,omitempty"`
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
