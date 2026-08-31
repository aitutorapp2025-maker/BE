package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartWaWorker subscribes to the WhatsApp queue and delivers each message via
// the Meta Business Cloud API. Jobs are always acked (never requeued): a parent
// report that fails (bad number, expired token) is logged and dropped — it must
// never retry forever or double-send.
func StartWaWorker(mq *queue.RabbitMQ, sender *wa.Provider, log *logger.Logger) error {
	return mq.Consume(wa.QueueWa, func(body []byte) error {
		var job wa.Job
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("wa worker: bad job payload: %v", err)
			return nil
		}
		if !sender.Enabled() {
			log.Errorf("wa worker: WhatsApp not configured — dropping message to %s", job.Phone)
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var err error
		if job.Kind == "otp" {
			err = sender.SendOTP(ctx, job.Phone, job.Code)
		} else {
			err = sender.SendText(ctx, job.Phone, job.Text)
		}
		if err != nil {
			log.Errorf("wa worker: send to %s: %v", job.Phone, err)
			return nil
		}
		log.Infof("wa worker: delivered to %s", job.Phone)
		return nil
	})
}
