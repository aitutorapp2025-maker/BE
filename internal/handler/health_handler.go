// Package handler contains the HTTP handlers (controllers) for the API.
package handler

import (
	"context"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// HealthHandler reports the health of the service and its dependencies.
type HealthHandler struct {
	db    *gorm.DB
	redis *redis.Client
	mq    *queue.RabbitMQ
}

// NewHealthHandler builds a HealthHandler.
func NewHealthHandler(db *gorm.DB, rdb *redis.Client, mq *queue.RabbitMQ) *HealthHandler {
	return &HealthHandler{db: db, redis: rdb, mq: mq}
}

// Check pings MySQL, Redis and RabbitMQ and returns a per-dependency status.
func (h *HealthHandler) Check(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	deps := fiber.Map{
		"postgres": h.checkPostgres(),
		"redis":    h.checkRedis(ctx),
		"rabbitmq": h.checkRabbitMQ(),
	}

	healthy := true
	for _, v := range deps {
		if v != "up" {
			healthy = false
		}
	}

	status := fiber.StatusOK
	if !healthy {
		status = fiber.StatusServiceUnavailable
	}

	return c.Status(status).JSON(fiber.Map{
		"success": healthy,
		"status": func() string {
			if healthy {
				return "ok"
			}
			return "degraded"
		}(),
		"services": deps,
		"time":     time.Now().Format(time.RFC3339),
	})
}

func (h *HealthHandler) checkPostgres() string {
	sqlDB, err := h.db.DB()
	if err != nil {
		return "down"
	}
	if err := sqlDB.Ping(); err != nil {
		return "down"
	}
	return "up"
}

func (h *HealthHandler) checkRedis(ctx context.Context) string {
	if err := h.redis.Ping(ctx).Err(); err != nil {
		return "down"
	}
	return "up"
}

func (h *HealthHandler) checkRabbitMQ() string {
	if h.mq == nil || h.mq.Conn == nil || h.mq.Conn.IsClosed() {
		return "down"
	}
	return "up"
}
