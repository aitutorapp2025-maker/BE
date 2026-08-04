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

	// OTPStatic is the fixed verification code used in development so no SMS is
	// sent and login can be tested offline. Ignored in production, where a real
	// random code is generated and delivered by the SMS gateway.
	OTPStatic string

	// GoogleClientID is the OAuth Web client ID that Google ID tokens are
	// issued for; the /student/google endpoint verifies the token's audience
	// against it. Public value (not a secret).
	GoogleClientID string

	DB       DBConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	JWT      JWTConfig
	SMTP     SMTPConfig
	AI       AIConfig
	Razorpay RazorpayConfig
	Uploads  UploadsConfig
}

// UploadsConfig controls where admin-uploaded assets (e.g. the landing-page OG
// image) are written and how their public URLs are built.
type UploadsConfig struct {
	Dir string // filesystem dir served at /uploads (default ./uploads)
	// PrivateDir holds NON-public uploads (e.g. student homework photos) that are
	// only served through a signed media route, never the /uploads static mount.
	PrivateDir string
	// PublicBaseURL is the absolute base used to build upload URLs
	// (e.g. https://api.vahaai.com). Empty = derive from the request host.
	PublicBaseURL string
}

// AIConfig holds the keys and model choices for the tutoring pipeline: Claude
// (answer generation) and Voyage (text embeddings for retrieval). When the keys
// are empty the AI features stay disabled and the rest of the app runs normally.
type AIConfig struct {
	AnthropicKey   string
	AnthropicModel string // e.g. claude-sonnet-5
	VoyageKey      string
	VoyageModel    string // e.g. voyage-3
	EmbedDim       int    // embedding dimension (must match the vector column)
	// EmbedProvider selects the embeddings backend: "voyage" (cloud API) or
	// "local" (self-hosted BGE-M3 via an Ollama-style /api/embed endpoint).
	EmbedProvider   string
	LocalEmbedURL   string // e.g. http://localhost:11434/api/embed (Ollama)
	LocalEmbedModel string // e.g. bge-m3
	// TopK is how many textbook passages to retrieve per question.
	TopK int
}

// Enabled reports whether the tutoring pipeline has the keys it needs.
func (a AIConfig) Enabled() bool { return a.AnthropicKey != "" && a.VoyageKey != "" }

// AIConfigFunc returns the current AI configuration. It's evaluated per-call so
// keys/models set in the admin panel take effect without a restart (the DB
// settings win over the environment fallback).
type AIConfigFunc func() AIConfig

// RazorpayConfig is the env fallback for the Razorpay keys (admin Settings win).
type RazorpayConfig struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
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
	Secret     string
	TTL        time.Duration // legacy/simple access-token lifetime
	AccessTTL  time.Duration // short-lived signed access token
	RefreshTTL time.Duration // rotating refresh token lifetime
	StudentTTL time.Duration // student (mobile) access-token lifetime
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
		AppName:   env("APP_NAME", "Vaha AI Backend"),
		AppEnv:    env("APP_ENV", "development"),
		AppPort:   env("APP_PORT", "8080"),
		OTPStatic:      env("OTP_STATIC", "202627"),
		GoogleClientID: env("GOOGLE_CLIENT_ID", ""),
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
			Secret:     env("JWT_SECRET", "dev-insecure-change-me-in-production"),
			TTL:        time.Duration(envInt("JWT_TTL_HOURS", 24)) * time.Hour,
			AccessTTL:  time.Duration(envInt("JWT_ACCESS_MINUTES", 15)) * time.Minute,
			RefreshTTL: time.Duration(envInt("JWT_REFRESH_HOURS", 168)) * time.Hour,
			// Students log in on mobile via OTP; keep them signed in for a while.
			StudentTTL: time.Duration(envInt("JWT_STUDENT_HOURS", 720)) * time.Hour,
		},
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", ""),
			Port:     env("SMTP_PORT", "587"),
			User:     env("SMTP_USER", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("SMTP_FROM", "support@vahaai.com"),
			FromName: env("SMTP_FROM_NAME", "Vaha AI"),
		},
		AI: AIConfig{
			AnthropicKey:   env("ANTHROPIC_API_KEY", ""),
			AnthropicModel: env("ANTHROPIC_MODEL", "claude-sonnet-5"),
			VoyageKey:      env("VOYAGE_API_KEY", ""),
			VoyageModel:     env("VOYAGE_MODEL", "voyage-3"),
			EmbedDim:        envInt("AI_EMBED_DIM", 1024),
			EmbedProvider:   env("EMBED_PROVIDER", "voyage"),
			LocalEmbedURL:   env("LOCAL_EMBED_URL", "http://localhost:11434/api/embed"),
			LocalEmbedModel: env("LOCAL_EMBED_MODEL", "bge-m3"),
			TopK:            envInt("AI_TOP_K", 6),
		},
		Razorpay: RazorpayConfig{
			KeyID:         env("RAZORPAY_KEY_ID", ""),
			KeySecret:     env("RAZORPAY_KEY_SECRET", ""),
			WebhookSecret: env("RAZORPAY_WEBHOOK_SECRET", ""),
		},
		Uploads: UploadsConfig{
			PrivateDir: env("UPLOADS_PRIVATE_DIR", "./uploads_private"),
			Dir:           env("UPLOADS_DIR", "./uploads"),
			PublicBaseURL: env("PUBLIC_BASE_URL", ""),
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
