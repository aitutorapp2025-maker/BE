package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
)

// CleanupAuditLogsJob deletes audit entries older than the retention window (90
// days). Runs once per calendar day.
func CleanupAuditLogsJob(audits *repository.AuditLogRepository) Job {
	return Job{
		Key:      "cleanup_audit_logs",
		Schedule: "daily",
		Run: func(now time.Time) (string, error) {
			n, err := audits.DeleteOlderThan(now.AddDate(0, 0, -90))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted %d old audit log(s)", n), nil
		},
	}
}

// ChargeDueMandatesJob triggers the recurring UPI auto-debit for students whose
// headless mandate is authorized and whose next charge is due. Idempotent (each
// debit is confirmed + granted via webhook), so it runs every tick.
func ChargeDueMandatesJob(payments *service.PaymentService) Job {
	return Job{
		Key:      "charge_due_mandates",
		Schedule: "hourly",
		Run: func(now time.Time) (string, error) {
			n, err := payments.ChargeDueMandates(now)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("charged %d mandate(s)", n), nil
		},
	}
}

// ExpireTrialsJob marks trials as expired once their window passes without
// autopay. Idempotent, so it runs every tick.
func ExpireTrialsJob(students *repository.StudentRepository) Job {
	return Job{
		Key:      "expire_trials",
		Schedule: "hourly",
		Run: func(now time.Time) (string, error) {
			n, err := students.ExpireOverdueTrials(now)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("expired %d trial(s)", n), nil
		},
	}
}

// TrialRemindersJob sends an FCM push to students whose free trial ends in 2, 1
// or 0 days. Runs once per day (the "daily" schedule), so each student gets at
// most one reminder per day.
func TrialRemindersJob(
	students *repository.StudentRepository,
	devices *repository.DeviceTokenRepository,
	push fcm.Pusher,
) Job {
	return Job{
		Key:      "trial_reminders",
		Schedule: "daily",
		Run: func(now time.Time) (string, error) {
			list, err := students.TrialsEndingWithin(now, 3)
			if err != nil {
				return "", err
			}
			if !push.Enabled() {
				return fmt.Sprintf("%d due but FCM not configured (0 sent)", len(list)), nil
			}
			sent := 0
			for _, st := range list {
				if st.TrialEndsAt == nil {
					continue
				}
				title, body := reminderText(daysUntil(*st.TrialEndsAt, now))
				if body == "" {
					continue
				}
				tokens, terr := devices.TokensForStudent(st.ID)
				if terr != nil || len(tokens) == 0 {
					continue
				}
				n, invalid, _ := push.SendToTokens(context.Background(), tokens, title, body, "",
					map[string]string{"type": "trial_reminder"})
				if len(invalid) > 0 {
					_ = devices.DeleteTokens(invalid)
				}
				if n > 0 {
					sent++
				}
			}
			return fmt.Sprintf("reminded %d of %d due", sent, len(list)), nil
		},
	}
}

// ReferralPromoJob broadcasts an FCM push to ALL customers every 3 days,
// nudging them to share Vaha AI with friends via refer & earn. It reuses the
// admin broadcast pipeline (queued on RabbitMQ; the push worker resolves every
// customer's device tokens, delivers and prunes stale ones). It only sends when
// the referral program is switched on, so a disabled program is never promoted.
func ReferralPromoJob(settings *repository.SettingRepository, push *fcm.Publisher) Job {
	return Job{
		Key:      "referral_promo",
		Schedule: "every3days",
		Run: func(now time.Time) (string, error) {
			s, err := settings.Get()
			if err != nil {
				return "", err
			}
			if !s.ReferralEnabled {
				return "referral program off — skipped", nil
			}
			if !push.Enabled() {
				return "FCM not configured (0 sent)", nil
			}
			title := "Invite friends, earn rewards 🎁"
			body := "Share Vaha AI with your friends and earn rewards when they join. Tap to open the app and get your link!"
			if s.ReferralRewardRupees > 0 {
				body = fmt.Sprintf(
					"Get ₹%d off your next bill for every friend who joins Vaha AI with your code. Open the app to share your link!",
					s.ReferralRewardRupees)
			}
			if err := push.Enqueue(fcm.PushJob{Title: title, Body: body}); err != nil {
				return "", err
			}
			return "queued referral promo to all customers", nil
		},
	}
}

// daysUntil is the calendar-day difference (t's date − now's date), so a trial
// ending later today is 0, tomorrow is 1, etc.
func daysUntil(t, now time.Time) int {
	d1 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	d2 := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	return int(d2.Sub(d1).Hours() / 24)
}

func reminderText(days int) (title, body string) {
	switch days {
	case 2:
		return "2 days left in your free trial",
			"Your Vaha AI free trial ends in 2 days. Subscribe to keep learning."
	case 1:
		return "1 day left in your free trial",
			"Your Vaha AI free trial ends tomorrow. Subscribe to keep learning."
	case 0:
		return "Last day of your free trial",
			"Today is the last day of your Vaha AI free trial. Subscribe now to keep learning."
	default:
		return "", ""
	}
}
