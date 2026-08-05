package worker

import (
	"context"
	"encoding/json"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartPushWorker subscribes to the push queue and delivers each admin broadcast:
// it resolves the target device tokens (all customers, or the chosen students),
// sends via FCM and prunes any tokens FCM reports as unregistered. Broadcasts are
// always acked (never requeued) so a partial failure can't cause double sends.
// Every job is also persisted to the notifications feed (notifs) so the app can
// show a list + unread count even for pushes that arrived while it was closed.
func StartPushWorker(mq *queue.RabbitMQ, push fcm.Pusher, devices *repository.DeviceTokenRepository, notifs *repository.NotificationRepository, log *logger.Logger) error {
	return mq.Consume(fcm.QueuePush, func(body []byte) error {
		var job fcm.PushJob
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("push worker: bad job payload: %v", err)
			return nil
		}

		// Persist to the feed first (a broadcast → one row with student_id 0; a
		// targeted send → one row per student) so it's recorded even if delivery
		// fails or no devices are registered. Best-effort.
		if notifs != nil {
			var rows []model.Notification
			if len(job.StudentIDs) == 0 {
				rows = append(rows, model.Notification{
					StudentID: 0, Title: job.Title, Body: job.Body,
					Image: job.Image, Type: "announcement"})
			} else {
				for _, id := range job.StudentIDs {
					rows = append(rows, model.Notification{
						StudentID: id, Title: job.Title, Body: job.Body,
						Image: job.Image, Type: "announcement"})
				}
			}
			if err := notifs.CreateBatch(rows); err != nil {
				log.Errorf("push worker: persist feed for %q: %v", job.Title, err)
			}
		}

		if !push.Enabled() {
			log.Errorf("push worker: FCM not configured — dropping %q", job.Title)
			return nil
		}

		var (
			tokens []string
			err    error
		)
		if len(job.StudentIDs) == 0 {
			tokens, err = devices.AllTokens()
		} else {
			tokens, err = devices.TokensForStudents(job.StudentIDs)
		}
		if err != nil {
			log.Errorf("push worker: load tokens for %q: %v", job.Title, err)
			return nil
		}
		if len(tokens) == 0 {
			log.Infof("push worker: no devices for %q", job.Title)
			return nil
		}

		sent, invalid, sendErr := push.SendToTokens(context.Background(), tokens,
			job.Title, job.Body, job.Image,
			map[string]string{"type": "admin_broadcast"})
		if len(invalid) > 0 {
			_ = devices.DeleteTokens(invalid)
		}
		log.Infof("push worker: %q → sent %d/%d, pruned %d stale token(s)",
			job.Title, sent, len(tokens), len(invalid))
		if sendErr != nil {
			log.Errorf("push worker: some sends failed for %q: %v", job.Title, sendErr)
		}
		return nil
	})
}
