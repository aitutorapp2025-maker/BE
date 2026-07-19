// Package email sends transactional email over SMTP. When SMTP is not
// configured, Send is a no-op (returns nil) so callers work in dev without mail.
// Configuration is read dynamically via a provider so it can come from the DB
// (admin settings) and change at runtime.
package email

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
)

// ConfigFunc returns the current SMTP configuration.
type ConfigFunc func() config.SMTPConfig

// Sender sends email via SMTP using the configuration returned by [get].
type Sender struct {
	get ConfigFunc
}

// New builds a Sender that reads its config from [get] on each call.
func New(get ConfigFunc) *Sender { return &Sender{get: get} }

// Enabled reports whether SMTP is currently configured.
func (s *Sender) Enabled() bool { return s.get().Enabled() }

// Send delivers an HTML email to a single recipient. Returns nil (no-op) when
// SMTP is disabled.
func (s *Sender) Send(to, subject, htmlBody string) error {
	cfg := s.get()
	if !cfg.Enabled() {
		return nil
	}
	return SendWith(cfg, to, subject, htmlBody)
}

// SendWith sends an HTML email using an explicit config, without checking the
// enabled flag. Used for the admin "test email" so credentials can be verified
// before turning sending on. Requires cfg.Host to be set.
func SendWith(cfg config.SMTPConfig, to, subject, htmlBody string) error {
	if cfg.Host == "" {
		return fmt.Errorf("smtp host is not set")
	}

	from := cfg.From
	fromHeader := from
	if cfg.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", cfg.FromName, from)
	}

	var b strings.Builder
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	msg := []byte(b.String())

	port := cfg.Port
	if port == "" {
		port = "587"
	}
	addr := cfg.Host + ":" + port

	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
	}

	// Implicit TLS (typically port 465).
	if port == "465" {
		return sendImplicitTLS(cfg.Host, addr, auth, from, to, msg)
	}

	// STARTTLS / plain (typically 587 or a local relay). smtp.SendMail issues
	// STARTTLS automatically when the server advertises it.
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

func sendImplicitTLS(host, addr string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
