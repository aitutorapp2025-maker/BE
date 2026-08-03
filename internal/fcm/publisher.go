package fcm

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueuePush is the RabbitMQ queue name for outgoing admin push broadcasts.
const QueuePush = "push.send"

// PushJob is an admin push broadcast, published to RabbitMQ and delivered by the
// push worker — which resolves the device tokens, sends via FCM and prunes any
// tokens FCM reports as unregistered.
type PushJob struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Image      string `json:"image,omitempty"`
	StudentIDs []uint `json:"student_ids,omitempty"` // empty = all customers
}

// Publisher enqueues push jobs onto RabbitMQ so the admin HTTP request returns
// immediately and delivery happens in the background worker.
type Publisher struct {
	mq      *queue.RabbitMQ
	enabled func() bool
}

// NewPublisher builds a Publisher. enabled is evaluated per-call so an uploaded
// service account takes effect without a restart.
func NewPublisher(mq *queue.RabbitMQ, enabled func() bool) *Publisher {
	return &Publisher{mq: mq, enabled: enabled}
}

// Enabled reports whether FCM push is currently configured.
func (p *Publisher) Enabled() bool { return p.enabled() }

// Enqueue publishes a push job to the queue.
func (p *Publisher) Enqueue(job PushJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.mq.Publish(QueuePush, body)
}
