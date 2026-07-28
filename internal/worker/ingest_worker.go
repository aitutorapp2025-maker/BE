package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartIngestWorker subscribes to the book-ingestion queue and indexes each
// book's text into vector chunks. A failure alerts the admin; the job is dropped
// (not requeued) so a poison book can't loop forever — the admin re-triggers
// ingestion from the book page.
func StartIngestWorker(mq *queue.RabbitMQ, tutor *service.TutorService, alerter *alert.Alerter, log *logger.Logger) error {
	return mq.Consume(ai.QueueIngest, func(body []byte) error {
		var job ai.IngestJob
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("ingest worker: bad job payload: %v", err)
			return nil // drop malformed
		}
		// Embedding a whole book can take a while; give it a generous ceiling.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := tutor.Ingest(ctx, job.BookID, job.Content); err != nil {
			log.Errorf("ingest worker: book %d failed: %v", job.BookID, err)
			alerter.Notify("Book indexing failure", fmt.Sprintf(
				`<p>Indexing failed for book <strong>#%d</strong>.</p><p>Error: %s</p>`,
				job.BookID, err.Error()))
			return err // nack (dropped)
		}
		log.Infof("ingest worker: indexed book %d", job.BookID)
		return nil
	})
}
