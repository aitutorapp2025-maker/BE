package repository

import (
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
)

// AuditBuffer batches audit-log writes: instead of one INSERT (in its own
// goroutine) per request, entries are queued on a channel and flushed in
// batches — every 100 entries or every 2 seconds, whichever comes first. This
// collapses a burst of tiny per-request writes into a few multi-row INSERTs.
//
// Enqueue is non-blocking and lossy under extreme pressure (a full buffer drops
// the entry rather than slowing the request path) — acceptable for a request
// audit trail. A handful of buffered entries may be lost on shutdown.
type AuditBuffer struct {
	repo *AuditLogRepository
	ch   chan model.AuditLog
}

// NewAuditBuffer builds an AuditBuffer and starts its background flusher.
func NewAuditBuffer(repo *AuditLogRepository) *AuditBuffer {
	b := &AuditBuffer{repo: repo, ch: make(chan model.AuditLog, 2048)}
	go b.loop()
	return b
}

// Enqueue adds an entry to the buffer without blocking. If the buffer is full
// the entry is dropped (protecting the request path).
func (b *AuditBuffer) Enqueue(a model.AuditLog) {
	select {
	case b.ch <- a:
	default: // buffer full — drop rather than block the request
	}
}

// loop drains the channel, flushing on a size or time threshold.
func (b *AuditBuffer) loop() {
	const maxBatch = 100
	const flushEvery = 2 * time.Second

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]model.AuditLog, 0, maxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = b.repo.RecordBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case a := <-b.ch:
			batch = append(batch, a)
			if len(batch) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
