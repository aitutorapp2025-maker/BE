package ai

import (
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
)

// QueueIngest is the RabbitMQ queue name for book-ingestion jobs.
const QueueIngest = "books.ingest"

// IngestJob asks the worker to (re)index a book's text into vector chunks.
type IngestJob struct {
	BookID  uint   `json:"book_id"`
	Content string `json:"content"`
}

// IngestPublisher enqueues book-ingestion jobs so the HTTP request returns
// immediately while embedding happens in the background worker.
type IngestPublisher struct {
	mq      *queue.RabbitMQ
	enabled func() bool
}

// NewIngestPublisher builds an IngestPublisher. enabled is evaluated per-call
// (the AI keys must be present for ingestion to run).
func NewIngestPublisher(mq *queue.RabbitMQ, enabled func() bool) *IngestPublisher {
	return &IngestPublisher{mq: mq, enabled: enabled}
}

// Enabled reports whether the tutoring pipeline is configured.
func (p *IngestPublisher) Enabled() bool { return p.enabled() }

// Enqueue publishes an ingestion job onto RabbitMQ.
func (p *IngestPublisher) Enqueue(job IngestJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.mq.Publish(QueueIngest, body)
}
