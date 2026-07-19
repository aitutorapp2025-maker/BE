package service

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// SMTPProvider returns an email.ConfigFunc that reads SMTP settings from the DB
// (admin settings). When the DB has no enabled SMTP config, it falls back to the
// environment-provided config.
func SMTPProvider(settings *repository.SettingRepository, envFallback config.SMTPConfig) email.ConfigFunc {
	return smtpProvider(settings, envFallback, false)
}

// SMTPProviderForce is like SMTPProvider but ignores the "enabled" toggle — it
// returns the DB config whenever a host is set. Used by the "test email" so
// credentials can be verified before turning sending on.
func SMTPProviderForce(settings *repository.SettingRepository, envFallback config.SMTPConfig) email.ConfigFunc {
	return smtpProvider(settings, envFallback, true)
}

func smtpProvider(settings *repository.SettingRepository, envFallback config.SMTPConfig, ignoreEnabled bool) email.ConfigFunc {
	return func() config.SMTPConfig {
		s, err := settings.Get()
		if err == nil && s.SmtpHost != "" && (ignoreEnabled || s.SmtpEnabled) {
			port := s.SmtpPort
			if port == "" {
				port = "587"
			}
			from := s.SmtpFrom
			if from == "" {
				from = s.SmtpUser
			}
			return config.SMTPConfig{
				Host:     s.SmtpHost,
				Port:     port,
				User:     s.SmtpUser,
				Password: s.SmtpPassword,
				From:     from,
				FromName: s.SmtpFromName,
			}
		}
		return envFallback
	}
}
