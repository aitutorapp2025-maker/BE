package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/database"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"gorm.io/gorm"
)

// CleanupAuditLogsJob enforces the 90-day audit retention window. Runs once per
// calendar day. On the monthly-partitioned audit_logs table it pre-creates the
// upcoming partitions, then DROPs every partition whose whole month is past the
// cutoff — instant, instead of deleting millions of rows. (A row therefore
// lives until its month ages out: up to ~30 days past the 90-day cutoff.)
// Falls back to a plain DELETE if the table was never converted (partition
// migration failed at boot).
func CleanupAuditLogsJob(db *gorm.DB) Job {
	return Job{
		Key:      "cleanup_audit_logs",
		Schedule: "daily",
		Run: func(now time.Time) (string, error) {
			cutoff := now.AddDate(0, 0, -90)
			partitioned, err := database.IsAuditPartitioned(db)
			if err != nil {
				return "", err
			}
			if !partitioned {
				n, err := database.DeleteAuditOlderThan(db, cutoff)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("deleted %d old audit log(s) (unpartitioned fallback)", n), nil
			}
			if err := database.EnsureAuditPartitions(db, now); err != nil {
				return "", err
			}
			dropped, err := database.DropOldAuditPartitions(db, cutoff)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("dropped %d expired audit partition(s)", dropped), nil
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

// SyncFirebaseStatsJob pulls the Firebase Analytics + Crashlytics BigQuery
// exports into our daily-aggregate tables once per day, so the admin dashboards
// read from our own DB. It syncs the last few days (to fill the export's landing
// delay + any gaps) and no-ops when both features are switched off / unconfigured.
func SyncFirebaseStatsJob(stats *service.FirebaseStatsService) Job {
	return Job{
		Key:      "sync_firebase_stats",
		Schedule: "daily",
		Run: func(now time.Time) (string, error) {
			return stats.Sync(context.Background(), 3)
		},
	}
}

// TaskRemindersJob sends the "time to study" push when a homework task's
// scheduled time arrives. Runs every scheduler tick (minutely) so reminders
// land close to the time the student picked; each task is pushed at most once
// (reminder_sent_at) and stale reminders (>30 min old) are never sent.
func TaskRemindersJob(
	homeworks *repository.HomeworkRepository,
	devices *repository.DeviceTokenRepository,
	push fcm.Pusher,
) Job {
	return Job{
		Key:      "homework_task_reminders",
		Schedule: "minutely",
		Run: func(now time.Time) (string, error) {
			due, err := homeworks.DueTaskReminders(now)
			if err != nil {
				return "", err
			}
			if len(due) == 0 {
				return "0 due", nil
			}
			if !push.Enabled() {
				return fmt.Sprintf("%d due but FCM not configured (0 sent)", len(due)), nil
			}
			sent := 0
			done := make([]uint, 0, len(due))
			for _, d := range due {
				// Mark first regardless of delivery — a broken token must never
				// make the same reminder retry every minute.
				done = append(done, d.TaskID)
				tokens, terr := devices.TokensForStudent(d.StudentID)
				if terr != nil || len(tokens) == 0 {
					continue
				}
				body := fmt.Sprintf("It's time for: %s", d.TaskTitle)
				if s := d.Subject; s != "" {
					body = fmt.Sprintf("It's time for: %s (%s)", d.TaskTitle, s)
				}
				n, invalid, _ := push.SendToTokens(context.Background(), tokens,
					"Time to study 📚", body, "",
					map[string]string{"type": "task_reminder"})
				if len(invalid) > 0 {
					_ = devices.DeleteTokens(invalid)
				}
				if n > 0 {
					sent++
				}
			}
			if err := homeworks.MarkRemindersSent(done, now); err != nil {
				return "", err
			}
			return fmt.Sprintf("reminded %d of %d due", sent, len(due)), nil
		},
	}
}

// ParentDailyReportJob WhatsApps each parent their child's study report for the
// day — tasks completed, tests taken and the day's score. Runs once per day
// after 7 PM ("daily@19"). The cron only composes + enqueues on RabbitMQ; the
// WhatsApp worker delivers in the background (same pattern as email/SMS/push).
// Students with no activity that day (or no phone on file) are skipped, and the
// whole job no-ops until WhatsApp is configured in admin Settings.
func ParentDailyReportJob(
	homeworks *repository.HomeworkRepository,
	students *repository.StudentRepository,
	waPub *wa.Publisher,
) Job {
	return Job{
		Key:      "parent_daily_reports",
		Schedule: "daily@19",
		Run: func(now time.Time) (string, error) {
			if !waPub.Enabled() {
				return "WhatsApp not configured (0 sent)", nil
			}
			from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			summaries, err := homeworks.DailySummaries(from, now)
			if err != nil {
				return "", err
			}
			if len(summaries) == 0 {
				return "no student activity today (0 sent)", nil
			}
			queued, skipped := 0, 0
			for id, sum := range summaries {
				st, serr := students.FindByID(id)
				if serr != nil || st == nil {
					skipped++
					continue
				}
				phone := st.ParentPhone
				if phone == "" {
					phone = st.Phone // fall back so the family still gets the report
				}
				if phone == "" {
					skipped++
					continue
				}
				if err := waPub.Enqueue(wa.Job{Phone: phone, Text: composeParentReport(st.Name, now, sum)}); err != nil {
					return "", err
				}
				queued++
			}
			return fmt.Sprintf("queued %d report(s), skipped %d, %d student(s) active", queued, skipped, len(summaries)), nil
		},
	}
}

// composeParentReport writes the parent-facing daily summary.
func composeParentReport(name string, day time.Time, s *repository.DailySummary) string {
	if name == "" {
		name = "Your child"
	}
	msg := fmt.Sprintf("📚 Vaha AI daily report — %s (%s)\n", name, day.Format("02 Jan 2006"))
	msg += fmt.Sprintf("✅ Tasks completed: %d\n", s.TasksDone)
	msg += fmt.Sprintf("📝 Tests taken: %d\n", s.TestsTaken)
	if s.MaxScore > 0 {
		pct := (s.Score*100 + s.MaxScore/2) / s.MaxScore
		msg += fmt.Sprintf("🏆 Today's score: %d/%d (%d%%)\n", s.Score, s.MaxScore, pct)
		switch {
		case pct >= 80:
			msg += "Excellent work today — please appreciate them! 🌟"
		case pct >= 50:
			msg += "Good progress today — a little more practice will help. 👍"
		default:
			msg += "They tried today — extra revision together will help a lot. 💪"
		}
	} else {
		msg += "Keep encouraging them to take the daily tests. 💪"
	}
	return msg
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
