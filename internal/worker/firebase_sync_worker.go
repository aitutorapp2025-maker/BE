package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartFirebaseSyncWorker subscribes to the firebase-stats queue and runs the
// BigQuery sync in the background, so the admin "Sync now" click never blocks on
// the (potentially slow) queries.
func StartFirebaseSyncWorker(mq *queue.RabbitMQ, stats *service.FirebaseStatsService, log *logger.Logger) error {
	return mq.Consume(service.QueueFirebaseSync, func(body []byte) error {
		var job service.SyncJob
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("firebase sync worker: bad job payload: %v", err)
			return nil // drop malformed job
		}
		days := job.Days
		if days <= 0 {
			days = 3
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		msg, err := stats.Sync(ctx, days)
		if err != nil {
			// Ack anyway (return nil) — retrying immediately won't help a config/
			// permission error; the daily cron + next manual click will retry.
			log.Errorf("firebase sync worker: %v (%s)", err, msg)
			return nil
		}
		log.Infof("firebase sync worker: %s", msg)
		return nil
	})
}
