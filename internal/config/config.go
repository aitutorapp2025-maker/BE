// Package config loads runtime configuration from environment variables (and a
// local .env file in development). All settings have sensible localhost
// defaults so the app boots against the XAMPP MySQL / local Redis / RabbitMQ.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the service.
type Config struct {
	AppName string
	AppEnv  string
	AppPort string

	DB       DBConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	JWT      JWTConfig
	SMTP     SMTPConfig
}

// SMTPConfig holds outgoing email (SMTP) settings. When Host is empty, email
// sending is disabled (submissions are still saved).
type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	FromName string
}

// Enabled reports whether outgoing email is configured.
func (s SMTPConfig) Enabled() bool { return s.Host != "" }

// JWTConfig holds JSON Web Token signing settings.
type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

// DBConfig holds PostgreSQL connection settings.
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	TimeZone string
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// RabbitMQConfig holds RabbitMQ connection settings.
type RabbitMQConfig struct {
	URL string
}

// IsProduction reports whether the app runs in the production environment.
func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// DSN builds the GORM/PostgreSQL data source name (pgx/lib-pq key-value form).
func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode, d.TimeZone)
}

// Addr returns the Redis host:port.
func (r RedisConfig) Addr() string { return r.Host + ":" + r.Port }

// Load reads configuration from the environment. A .env file, if present, is
// loaded first (without overriding real environment variables).
func Load() Config {
	// Best-effort: ignore the error when no .env exists (e.g. production).
	_ = godotenv.Load()

	return Config{
		AppName: env("APP_NAME", "Vaha AI Backend"),
		AppEnv:  env("APP_ENV", "development"),
		AppPort: env("APP_PORT", "8080"),
		DB: DBConfig{
			Host:     env("DB_HOST", "127.0.0.1"),
			Port:     env("DB_PORT", "5432"),
			User:     env("DB_USER", "postgres"),
			Password: env("DB_PASSWORD", "postgres"),
			Name:     env("DB_NAME", "vaha_ai"),
			SSLMode:  env("DB_SSLMODE", "disable"),
			TimeZone: env("DB_TIMEZONE", "Asia/Kolkata"),
		},
		Redis: RedisConfig{
			Host:     env("REDIS_HOST", "127.0.0.1"),
			Port:     env("REDIS_PORT", "6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		RabbitMQ: RabbitMQConfig{
			URL: env("RABBITMQ_URL", "amqp://guest:guest@127.0.0.1:5672/"),
		},
		JWT: JWTConfig{
			Secret: env("JWT_SECRET", "dev-insecure-change-me-in-production"),
			TTL:    time.Duration(envInt("JWT_TTL_HOURS", 24)) * time.Hour,
		},
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", ""),
			Port:     env("SMTP_PORT", "587"),
			User:     env("SMTP_USER", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("SMTP_FROM", "support@vahaai.com"),
			FromName: env("SMTP_FROM_NAME", "Vaha AI"),
		},
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
