// Package server builds the Fiber application: middleware, dependency wiring and
// route registration.
package server

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
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
}

// New builds and configures the Fiber app with all middleware and routes.
func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               d.Cfg.AppName,
		DisableStartupMessage: true,
		ErrorHandler:          errorHandler(d.Alerter, d.Log),
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

	registerRoutes(app, d)
	return app
}

// errorHandler renders errors as a consistent JSON envelope and emails an alert
// for server errors (5xx).
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
		return c.Status(code).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}
}
