package worker

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
	"github.com/redis/go-redis/v9"
)

// waSendRate caps how many WhatsApp messages we send per second (Meta enforces
// per-account throughput tiers). Paced globally via Redis so it holds even
// across restarts / multiple workers.
const waSendRate = 10

// StartWaWorker subscribes to the WhatsApp queue and delivers each message via
// the Meta Business Cloud API. Jobs are always acked (never requeued): a send
// that fails (bad number, expired token) is logged/recorded and dropped so it
// can't retry forever or double-send. Free-form and OTP sends are recorded in
// the inbox; template (campaign) sends update the per-recipient status.
func StartWaWorker(mq *queue.RabbitMQ, sender *wa.Provider,
	messages *repository.WaMessageRepository,
	campaigns *repository.WaCampaignRepository,
	rdb *redis.Client, log *logger.Logger) error {
	return mq.Consume(wa.QueueWa, func(body []byte) error {
		var job wa.Job
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("wa worker: bad job payload: %v", err)
			return nil
		}
		if !sender.Enabled() {
			log.Errorf("wa worker: WhatsApp not configured — dropping message to %s", job.Phone)
			if job.Kind == "template" && campaigns != nil && job.RecipientID > 0 {
				_ = campaigns.MarkFailed(job.RecipientID, job.CampaignID, "WhatsApp not configured")
			}
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// ── Campaign template send ──────────────────────────────────────────
		if job.Kind == "template" {
			waPace(ctx, rdb, waSendRate) // respect Meta's throughput tier
			id, err := sender.SendTemplate(ctx, wa.TemplateMessage{
				Phone:         job.Phone,
				Name:          job.Template,
				Lang:          job.Lang,
				HeaderImageID: job.HeaderImageID,
				BodyParams:    job.Params,
			})
			if err != nil {
				log.Errorf("wa worker: campaign send to %s: %v", job.Phone, err)
				if campaigns != nil && job.RecipientID > 0 {
					_ = campaigns.MarkFailed(job.RecipientID, job.CampaignID, err.Error())
				}
				return nil
			}
			if campaigns != nil && job.RecipientID > 0 {
				_ = campaigns.MarkSent(job.RecipientID, job.CampaignID, id)
			}
			if messages != nil {
				_ = messages.Save(&model.WaMessage{
					Phone: inboxPhone(job.Phone), Direction: "out",
					MsgType: "template", Text: "📣 Campaign: " + job.Template,
				})
			}
			return nil
		}

		// ── OTP / free-form ─────────────────────────────────────────────────
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

// waPace blocks briefly so no more than perSec sends happen in any one-second
// window, using a Redis fixed-window counter shared across workers/restarts.
// A nil client (or Redis error) means no pacing.
func waPace(ctx context.Context, rdb *redis.Client, perSec int) {
	if rdb == nil || perSec <= 0 {
		return
	}
	for i := 0; i < 50; i++ { // bounded (~10s max) so a wedged worker can't hang forever
		key := "wa:rate:" + strconv.FormatInt(time.Now().Unix(), 10)
		n, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			return
		}
		if n == 1 {
			rdb.Expire(ctx, key, 2*time.Second)
		}
		if n <= int64(perSec) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
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
