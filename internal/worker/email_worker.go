// Package worker holds background consumers that process RabbitMQ jobs.
package worker

import (
	"encoding/json"
	"fmt"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartEmailWorker subscribes to the email queue and sends each job via SMTP.
// A failed delivery raises an alert (unless the job is itself an alert email).
// [forceSender] sends Force jobs (test emails) even when SMTP is toggled off.
func StartEmailWorker(mq *queue.RabbitMQ, sender, forceSender *email.Sender, alerter *alert.Alerter, log *logger.Logger) error {
	return mq.Consume(email.QueueSend, func(body []byte) error {
		var job email.Job
		if err := json.Unmarshal(body, &job); err != nil {
			// Malformed message — drop it (returning nil acks/removes it).
			log.Errorf("email worker: bad job payload: %v", err)
			return nil
		}
		s := sender
		if job.Force {
			s = forceSender
		}
		if err := s.Send(job.To, job.Subject, job.HTML); err != nil {
			log.Errorf("email worker: send to %s failed: %v", job.To, err)
			if !job.NoAlert {
				alerter.Notify("Email delivery failure", fmt.Sprintf(
					`<p>The email worker failed to deliver a message to <strong>%s</strong>.</p><p>Error: %s</p>`,
					email.Escape(job.To), email.Escape(err.Error())))
			}
			return err // nack (dropped) so it doesn't loop
		}
		log.Infof("email worker: sent email to %s", job.To)
		return nil
	})
}
