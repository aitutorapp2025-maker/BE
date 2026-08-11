// Package server builds the Fiber application: middleware, dependency wiring and
// route registration.
package server

import (
	"encoding/json"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/scheduler"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Deps are the shared dependencies injected into handlers.
type Deps struct {
	Cfg     config.Config
	DB      *gorm.DB
	Redis   *redis.Client
	MQ      *queue.RabbitMQ
	Log     *logger.Logger
	SMTP    email.ConfigFunc // returns current SMTP config (DB settings, env fallback)
	SMS     sms.ConfigFunc   // returns current SMS config (DB settings)
	Alerter *alert.Alerter   // emails an admin on server errors
	Push    fcm.Pusher       // FCM push sender (disabled when unconfigured)
	Sched   *scheduler.Scheduler    // background job runner (for admin "Run now")
	Payments *service.PaymentService // shared with the recurring-charge scheduler job
}

// New builds and configures the Fiber app with all middleware and routes.
func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               d.Cfg.AppName,
		DisableStartupMessage: true,
		ErrorHandler:          errorHandler(d.Alerter, d.Log),
		// Support/homework attachments travel as base64 inside the E2E-encrypted
		// body, which roughly doubles their size on the wire — a 12 MB file
		// becomes ~21 MB. Fiber's default 4 MB limit rejected them before the
		// handler ran ("unable to upload"). 32 MB comfortably covers the max.
		BodyLimit: 32 * 1024 * 1024,
		// Timeouts stop a slow or hung client from holding a connection (and its
		// goroutine + a pool slot) forever — slow-loris protection and pool safety.
		// ReadTimeout bounds how long we wait to receive the full request; IdleTimeout
		// bounds keep-alive reuse between requests. WriteTimeout is intentionally 0
		// (unlimited): the AI tutor answers over long-lived SSE streams, and any
		// non-zero WriteTimeout would cut a live answer off mid-stream.
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	})

	// recover turns panics into errors routed to the ErrorHandler above.
	app.Use(recover.New())
	app.Use(fiberlogger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, " +
			"X-Session, X-Nonce, X-Timestamp, X-Signature, X-Encrypted",
		// Let the browser read our custom response header (E2E flag).
		ExposeHeaders: "X-Encrypted",
	}))

	// Serve admin-uploaded assets (e.g. the landing OG image) from disk. These are
	// static files, so let browsers/CDNs cache them for a day to cut re-fetches.
	app.Use("/uploads", func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "public, max-age=86400")
		return c.Next()
	})
	app.Static("/uploads", d.Cfg.Uploads.Dir)

	registerRoutes(app, d)
	return app
}

// errorHandler renders errors as a consistent JSON envelope and emails an alert
// for server errors (5xx). When the request carried an E2E session key (stashed
// by the Encrypt middleware), the error body is encrypted too, so nothing is
// readable on the wire — matching the success responses.
func errorHandler(alerter *alert.Alerter, log *logger.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}
		if code >= fiber.StatusInternalServerError {
			log.Errorf("%s %s -> %d: %v", c.Method(), c.Path(), code, err)
			alerter.NotifyError(c.Method(), c.Path(), err.Error())
		}

		payload := fiber.Map{"success": false, "error": err.Error()}

		// Encrypt the error too when an E2E key was resolved for this request.
		if key, ok := c.Locals("enckey").([]byte); ok && len(key) > 0 {
			if raw, merr := json.Marshal(payload); merr == nil {
				if enc, eerr := cryptox.Encrypt(key, raw); eerr == nil {
					c.Status(code)
					c.Set("X-Encrypted", "1")
					c.Response().Header.SetContentType(fiber.MIMEApplicationJSON)
					return c.Send(enc)
				}
			}
		}

		return c.Status(code).JSON(payload)
	}
}
