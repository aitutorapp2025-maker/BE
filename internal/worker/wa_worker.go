package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartWaWorker subscribes to the WhatsApp queue and delivers each message via
// the Meta Business Cloud API. Jobs are always acked (never requeued): a parent
// report that fails (bad number, expired token) is logged and dropped — it must
// never retry forever or double-send. Successful sends are recorded in the
// inbox repository so the admin chat shows the outgoing side too.
func StartWaWorker(mq *queue.RabbitMQ, sender *wa.Provider,
	messages *repository.WaMessageRepository, log *logger.Logger) error {
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
		shown := job.Text
		if job.Kind == "otp" {
			err = sender.SendOTP(ctx, job.Phone, job.Code)
			shown = "🔐 Login code (OTP) sent" // never store the actual code
		} else {
			err = sender.SendText(ctx, job.Phone, job.Text)
		}
		if err != nil {
			log.Errorf("wa worker: send to %s: %v", job.Phone, err)
			return nil
		}
		if messages != nil {
			_ = messages.Save(&model.WaMessage{
				Phone:     inboxPhone(job.Phone),
				Direction: "out",
				MsgType:   "text",
				Text:      shown,
			})
		}
		log.Infof("wa worker: delivered to %s", job.Phone)
		return nil
	})
}

// inboxPhone normalizes to the digits-only international format Meta reports,
// so outgoing rows land in the same thread as the customer's replies.
func inboxPhone(p string) string {
	digits := make([]rune, 0, len(p))
	for _, r := range p {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	d := string(digits)
	if len(d) == 10 {
		return "91" + d
	}
	return d
}
