// Command api is the entrypoint for the Vaha AI backend HTTP service.
//
// It wires up configuration, MySQL (GORM), Redis and RabbitMQ, starts the Fiber
// server, and shuts everything down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/database"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/scheduler"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/server"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/worker"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

func main() {
	log := logger.New()
	cfg := config.Load()

	// ── MySQL ────────────────────────────────────────────────────────────
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	log.Infof("connected to PostgreSQL at %s:%s/%s", cfg.DB.Host, cfg.DB.Port, cfg.DB.Name)

	// Auto-migrate tables and seed a demo admin (idempotent).
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Infof("database migrated")

	// Performance indexes (composite indexes on hot queries). Best-effort — a
	// failure here is a warning, not fatal.
	if err := database.CreateIndexes(db); err != nil {
		log.Warnf("some performance indexes were not created: %v", err)
	}

	// Convert audit_logs to a monthly RANGE-partitioned table (retention becomes
	// a cheap partition DROP — essential at scale, where audit is the largest
	// table). Non-fatal: on failure the table stays plain and the cleanup cron
	// falls back to DELETE-based retention.
	if migrated, err := database.MigrateAuditPartitions(db); err != nil {
		log.Warnf("audit-log partitioning unavailable (DELETE retention fallback): %v", err)
	} else if migrated {
		log.Infof("audit_logs converted to monthly partitions")
	}

	// Vector store for the tutoring pipeline — only if pgvector is installed on
	// the PostgreSQL server. When it isn't, the AI features stay off but the app
	// still boots (the worker/route degrade to a clear "not configured" error).
	vectorsReady, err := database.MigrateVectors(db)
	if err != nil {
		log.Fatalf("migrate vectors: %v", err)
	}
	if vectorsReady {
		log.Infof("pgvector ready (book_chunks migrated)")
	} else {
		log.Infof("pgvector NOT installed — tutoring/RAG features disabled")
	}
	if seeded, err := database.SeedAdmin(db); err != nil {
		log.Fatalf("seed admin: %v", err)
	} else if seeded {
		log.Infof("seeded demo admin (admin@vahaai.com / Admin@123)")
	}
	if n, err := database.SeedStudents(db); err != nil {
		log.Fatalf("seed students: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d demo students", n)
	}
	if n, err := database.SeedClasses(db); err != nil {
		log.Fatalf("seed classes: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d classes", n)
	}
	if n, err := database.SeedClassGroups(db); err != nil {
		log.Fatalf("seed class groups: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d class groups", n)
	}
	if n, err := database.SeedBooks(db); err != nil {
		log.Fatalf("seed books: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d demo books", n)
	}
	if n, err := database.SeedPlans(db); err != nil {
		log.Fatalf("seed plans: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d plans", n)
	}
	// Backfill the ₹799/₹999/₹1299 tiers on databases that predate them.
	if n, err := database.EnsureStarterPlans(db); err != nil {
		log.Fatalf("ensure plans: %v", err)
	} else if n > 0 {
		log.Infof("added %d starter plans (₹799/₹999/₹1299)", n)
	}
	// Ensure one trial plan is flagged (for the onboarding default).
	if err := database.EnsureTrialPlan(db); err != nil {
		log.Fatalf("ensure trial plan: %v", err)
	}
	if seeded, err := database.SeedSettings(db); err != nil {
		log.Fatalf("seed settings: %v", err)
	} else if seeded {
		log.Infof("seeded default settings")
	}
	if seeded, err := database.SeedLanding(db); err != nil {
		log.Fatalf("seed landing: %v", err)
	} else if seeded {
		log.Infof("seeded landing-page content")
	}

	if seeded, err := database.SeedLegal(db); err != nil {
		log.Fatalf("seed legal: %v", err)
	} else if seeded {
		log.Infof("seeded terms & conditions")
	}

	if n, err := database.SeedTeachingLanguages(db); err != nil {
		log.Fatalf("seed teaching languages: %v", err)
	} else if n > 0 {
		log.Infof("seeded %d teaching languages", n)
	}

	// ── Redis ────────────────────────────────────────────────────────────
	rdb, err := cache.Connect(cfg)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Infof("connected to Redis at %s", cfg.Redis.Addr())

	// ── RabbitMQ ─────────────────────────────────────────────────────────
	mq, err := queue.Connect(cfg)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	log.Infof("connected to RabbitMQ")

	// SMTP config is read dynamically from the DB (admin settings) with the
	// environment as a fallback, so it can be changed at runtime.
	// Shared no-expiry Redis cache for read-mostly config/master data — the same
	// keys the HTTP layer (routes.go) uses, so a write there is seen here too.
	cacheStore := cache.NewStore(rdb)
	settingRepo := repository.NewSettingRepository(db, cacheStore)
	smtpProvider := service.SMTPProvider(settingRepo, cfg.SMTP)

	// Alerter emails an admin on server + background errors (shared by the HTTP
	// error handler and the email worker).
	alertMailer := email.NewPublisher(mq, func() bool { return smtpProvider().Enabled() })
	alerter := alert.New(settingRepo, rdb, alertMailer, log)

	// Background email worker — consumes queued email jobs and sends them.
	// forceSender ignores the "enabled" toggle (for test emails).
	emailSender := email.New(smtpProvider)
	forceSender := email.New(service.SMTPProviderForce(settingRepo, cfg.SMTP))
	if err := worker.StartEmailWorker(mq, emailSender, forceSender, alerter, log); err != nil {
		log.Fatalf("email worker: %v", err)
	}

	// Background SMS worker — consumes queued SMS jobs and sends them.
	smsProvider := service.SMSProvider(settingRepo)
	smsSender := sms.New(smsProvider)
	smsForceSender := sms.New(service.SMSProviderForce(settingRepo))
	if err := worker.StartSMSWorker(mq, smsSender, smsForceSender, alerter, log); err != nil {
		log.Fatalf("sms worker: %v", err)
	}
	log.Infof("sms worker started")
	if emailSender.Enabled() {
		log.Infof("email worker started (SMTP enabled)")
	} else {
		log.Infof("email worker started (SMTP disabled — configure it in admin settings)")
	}

	// Background book-ingestion worker — embeds uploaded textbooks into the
	// vector store. Started whenever pgvector is ready; the AI keys are read per
	// call from the admin Settings (env as fallback), so they can be added later
	// without a restart.
	if vectorsReady {
		aiProvider := service.AIProvider(settingRepo, cfg.AI)
		bookRepo := repository.NewBookRepository(db)
		bookChunkRepo := repository.NewBookChunkRepository(db)
		tutorService := service.NewTutorService(
			bookRepo, bookChunkRepo,
			ai.NewEmbedder(aiProvider, rdb),
			ai.NewChat(aiProvider),
			cfg.AI.TopK,
		)
		if err := worker.StartIngestWorker(mq, tutorService, alerter, log); err != nil {
			log.Fatalf("ingest worker: %v", err)
		}
		log.Infof("book ingestion worker started (AI keys via admin Settings / env)")
	} else {
		log.Infof("tutoring pipeline disabled (install pgvector to enable)")
	}

	// ── Background scheduler ─────────────────────────────────────────────
	// Jobs are enabled/disabled from the admin panel (cron_jobs table).
	studentRepo := repository.NewStudentRepository(db)
	cronRepo := repository.NewCronRepository(db)
	deviceRepo := repository.NewDeviceTokenRepository(db)

	// Credentials come from admin settings (uploaded service account) with the
	// environment as a fallback; the provider rebuilds when they change, so an
	// upload takes effect without a restart.
	pushSender := fcm.NewProvider(func() string {
		if s, err := settingRepo.Get(); err == nil &&
			strings.TrimSpace(s.FcmServiceAccount) != "" {
			return s.FcmServiceAccount
		}
		return fcm.EnvCredentials()
	})
	if pushSender.Enabled() {
		log.Infof("FCM push enabled")
	} else {
		log.Infof("FCM push disabled (upload a service account in admin Settings)")
	}

	// Push worker: delivers admin broadcasts queued on RabbitMQ (resolves tokens,
	// sends via FCM, prunes stale ones).
	if err := worker.StartPushWorker(mq, pushSender, deviceRepo, repository.NewNotificationRepository(db), log); err != nil {
		log.Errorf("push worker: %v", err)
	}

	// Firebase Analytics + Crashlytics BigQuery sync — shared by the daily cron
	// and the on-demand "Sync now" worker (so the admin click never blocks).
	firebaseStats := service.NewFirebaseStatsService(settingRepo, repository.NewFirebaseStatsRepository(db))
	if err := worker.StartFirebaseSyncWorker(mq, firebaseStats, log); err != nil {
		log.Errorf("firebase sync worker: %v", err)
	}

	// PaymentService is built here (not just in routes) so the recurring-charge
	// scheduler job and the HTTP handlers share one instance.
	razorpayProvider := service.RazorpayProvider(settingRepo, cfg.Razorpay)
	razorpayClient := payment.NewClient(razorpayProvider)
	paymentService := service.NewPaymentService(
		razorpayClient, razorpayProvider,
		studentRepo, repository.NewPlanRepository(db, cacheStore),
		service.NewCreditService(repository.NewCreditRepository(db)),
		repository.NewPaymentEventRepository(db))

	// Publisher for scheduler-driven broadcasts (reuses the admin push pipeline:
	// queued on RabbitMQ, delivered + pruned by the push worker).
	pushPublisher := fcm.NewPublisher(mq, func() bool { return pushSender.Enabled() })

	// WhatsApp (Meta Business Cloud API) for the parents' daily study report.
	// Config comes live from admin Settings, so pasting the token applies
	// without a restart.
	waSender := wa.NewProvider(func() wa.Config {
		s, err := settingRepo.Get()
		if err != nil {
			return wa.Config{}
		}
		return wa.Config{
			Enabled:      s.WhatsappEnabled,
			Token:        s.WhatsappToken,
			PhoneID:      s.WhatsappPhoneID,
			Template:     s.WhatsappTemplate,
			TemplateLang: s.WhatsappTemplateLang,
			CountryCode:  s.SmsCountryCode,
		}
	})
	// The report cron only enqueues; this worker delivers in the background.
	waPublisher := wa.NewPublisher(mq, waSender.Enabled)
	if err := worker.StartWaWorker(mq, waSender, log); err != nil {
		log.Errorf("wa worker: %v", err)
	}

	homeworkRepo := repository.NewHomeworkRepository(db)

	sched := scheduler.New(cronRepo, log)
	registrations := []struct {
		job  scheduler.Job
		name string
		desc string
	}{
		{scheduler.ExpireTrialsJob(studentRepo),
			"Expire trials",
			"Marks a free trial expired when it ends with no active autopay."},
		{scheduler.TrialRemindersJob(studentRepo, deviceRepo, pushSender),
			"Trial ending reminders",
			"Sends an FCM push 2, 1 and 0 days before a free trial ends."},
		{scheduler.ChargeDueMandatesJob(paymentService),
			"Charge due autopay mandates",
			"Auto-debits due UPI-AutoPay mandates (headless flow) and grants plan credits."},
		{scheduler.CleanupAuditLogsJob(db),
			"Cleanup audit logs",
			"Drops audit-log partitions older than the 90-day retention window."},
		{scheduler.ReferralPromoJob(settingRepo, pushPublisher),
			"Referral promo push",
			"Every 3 days, sends all customers an FCM push promoting refer & earn (only when the referral program is on)."},
		{scheduler.SyncFirebaseStatsJob(firebaseStats),
			"Sync Firebase analytics + crashlytics",
			"Once a day, pulls the Firebase Analytics + Crashlytics BigQuery export into our dashboards (only when enabled + configured)."},
		{scheduler.TaskRemindersJob(homeworkRepo, deviceRepo, pushSender),
			"Homework study-time reminders",
			"Every minute, pushes 'Time to study' when a homework task's scheduled time arrives (each task reminded once; re-armed when the student moves the time)."},
		{scheduler.ParentDailyReportJob(homeworkRepo, studentRepo, waPublisher),
			"Parents' daily WhatsApp report",
			"Once a day after 7 PM, WhatsApps each parent their child's study report — tasks completed, tests taken and the day's score (needs WhatsApp configured in Settings)."},
	}
	for _, r := range registrations {
		if err := cronRepo.Ensure(model.CronJob{
			Key: r.job.Key, Name: r.name, Description: r.desc, Schedule: r.job.Schedule,
		}); err != nil {
			log.Errorf("seed cron %s: %v", r.job.Key, err)
		}
		sched.Register(r.job)
	}
	// Minute tick so study-time reminders land on time; hourly/daily jobs keep
	// their own cadence via per-schedule gating in the scheduler.
	sched.Start(time.Minute)
	log.Infof("scheduler started (%d job(s), admin-managed)", len(registrations))

	// ── HTTP server ──────────────────────────────────────────────────────
	app := server.New(server.Deps{
		Cfg:     cfg,
		DB:      db,
		Redis:   rdb,
		MQ:      mq,
		Log:     log,
		SMTP:    smtpProvider,
		SMS:     smsProvider,
		Alerter:  alerter,
		Push:     pushSender,
		Sched:    sched,
		Payments: paymentService,
	})

	go func() {
		addr := ":" + cfg.AppPort
		log.Infof("%s listening on %s (%s)", cfg.AppName, addr, cfg.AppEnv)
		if err := app.Listen(addr); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Infof("shutting down...")
	sched.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		log.Errorf("server shutdown: %v", err)
	}
	mq.Close()
	if err := rdb.Close(); err != nil {
		log.Errorf("redis close: %v", err)
	}
	if err := database.Close(db); err != nil {
		log.Errorf("postgres close: %v", err)
	}
	log.Infof("bye")
}
