// Package alert emails an administrator when a server error occurs. Alerts are
// throttled via Redis so a burst of the same error can't flood the inbox.
package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
	"github.com/redis/go-redis/v9"
)

// throttle is how long the same error signature is suppressed after an alert.
const throttle = 5 * time.Minute

// Alerter sends error-notification emails to the configured alert address.
type Alerter struct {
	settings *repository.SettingRepository
	redis    *redis.Client
	mailer   *email.Publisher
	log      *logger.Logger
}

// New builds an Alerter.
func New(settings *repository.SettingRepository, rdb *redis.Client, mailer *email.Publisher, log *logger.Logger) *Alerter {
	return &Alerter{settings: settings, redis: rdb, mailer: mailer, log: log}
}

// NotifyError reports a request-level server error. Non-blocking.
func (a *Alerter) NotifyError(method, path, errMsg string) {
	endpoint := method + " " + path
	body := fmt.Sprintf(`<p>An error occurred in the Vaha AI backend.</p>
<table cellpadding="6" style="border-collapse:collapse;font-size:14px;">
  <tr><td style="color:#5E6B63;">Endpoint</td><td><strong>%s</strong></td></tr>
  <tr><td style="color:#5E6B63;">Error</td><td>%s</td></tr>
</table>`, email.Escape(endpoint), email.Escape(errMsg))
	a.Notify("Server error: "+endpoint, body)
}

// Notify sends a general alert email with the given short subject and HTML body.
// It is non-blocking, throttled by subject, and safe to call from background
// workers. [subject] is used as the throttle key, so keep it stable for the same
// class of failure.
func (a *Alerter) Notify(subject, bodyHTML string) {
	go a.dispatch(subject, bodyHTML)
}

func (a *Alerter) dispatch(subject, bodyHTML string) {
	s, err := a.settings.Get()
	if err != nil {
		return
	}
	if !s.ErrorAlertsEnabled || s.AlertEmail == "" || !a.mailer.Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Throttle by subject so a burst of the same failure sends at most one email
	// per [throttle] window. On a Redis error, alert anyway.
	key := "alert:" + subject
	if ok, rerr := a.redis.SetNX(ctx, key, "1", throttle).Result(); rerr == nil && !ok {
		return
	}

	footer := fmt.Sprintf(
		`<p style="color:#5E6B63;font-size:13px;margin-top:16px;">Time: %s · Further identical alerts are muted for %d minutes.</p>`,
		time.Now().Format(time.RFC1123), int(throttle.Minutes()))

	job := email.Job{
		To:      s.AlertEmail,
		Subject: "[Vaha AI] " + subject,
		HTML:    email.Wrap("⚠️ Application alert", bodyHTML+footer),
		NoAlert: true, // never alert about a failed alert (avoids loops)
	}
	if err := a.mailer.Enqueue(job); err != nil {
		a.log.Errorf("alerter: failed to queue alert email: %v", err)
	}
}
