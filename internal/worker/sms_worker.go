package worker

import (
	"encoding/json"
	"fmt"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartSMSWorker subscribes to the SMS queue and sends each job via the
// configured provider. [forceSender] sends Force jobs (test SMS) even when SMS
// is toggled off.
func StartSMSWorker(mq *queue.RabbitMQ, sender, forceSender *sms.Sender, alerter *alert.Alerter, log *logger.Logger) error {
	return mq.Consume(sms.QueueSend, func(body []byte) error {
		var job sms.Job
		if err := json.Unmarshal(body, &job); err != nil {
			log.Errorf("sms worker: bad job payload: %v", err)
			return nil
		}
		s := sender
		if job.Force {
			s = forceSender
		}
		if err := s.Send(job.To, job.Text); err != nil {
			log.Errorf("sms worker: send to %s failed: %v", job.To, err)
			if !job.Force {
				alerter.Notify("SMS delivery failure", fmt.Sprintf(
					`<p>The SMS worker failed to deliver a message to <strong>%s</strong>.</p><p>Error: %s</p>`,
					job.To, err.Error()))
			}
			return err
		}
		log.Infof("sms worker: sent SMS to %s", job.To)
		return nil
	})
}
