// Command api is the entrypoint for the Vaha AI backend HTTP service.
//
// It wires up configuration, MySQL (GORM), Redis and RabbitMQ, starts the Fiber
// server, and shuts everything down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/database"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/server"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
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
	settingRepo := repository.NewSettingRepository(db)
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

	// ── HTTP server ──────────────────────────────────────────────────────
	app := server.New(server.Deps{
		Cfg:     cfg,
		DB:      db,
		Redis:   rdb,
		MQ:      mq,
		Log:     log,
		SMTP:    smtpProvider,
		SMS:     smsProvider,
		Alerter: alerter,
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
