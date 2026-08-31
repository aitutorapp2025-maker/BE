// Package wa sends WhatsApp messages through Meta's WhatsApp Business Cloud
// API (graph.facebook.com). It powers the parents' daily study report. The
// admin pastes the permanent access token + phone number ID (and optionally an
// approved template name) in Settings; when unconfigured every send is a no-op
// error so callers can degrade gracefully.
package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const graphBaseURL = "https://graph.facebook.com/v20.0/"

// Config is the live WhatsApp configuration (admin Settings win over env).
type Config struct {
	Enabled      bool
	Token        string // permanent access token (System User)
	PhoneID      string // WhatsApp Business phone number ID
	Template     string // approved template with ONE body {{1}} parameter; empty = free-form text
	TemplateLang string // template language code, e.g. "en" or "ta"
	CountryCode  string // default country code for bare 10-digit numbers, e.g. "91"
	// OTP over WhatsApp: an APPROVED Authentication-category template (Meta
	// auto-generates its body + copy-code button; we only fill the code).
	OtpEnabled  bool
	OtpTemplate string // e.g. "otp_code"
	OtpLang     string // e.g. "en"
}

// OtpReady reports whether OTPs can be sent over WhatsApp.
func (c Config) OtpReady() bool {
	return c.Ready() && c.OtpEnabled && strings.TrimSpace(c.OtpTemplate) != ""
}

// Ready reports whether messages can actually be sent.
func (c Config) Ready() bool {
	return c.Enabled && strings.TrimSpace(c.Token) != "" && strings.TrimSpace(c.PhoneID) != ""
}

// Provider resolves the config per call (admin Settings edits apply without a
// restart) and sends messages.
type Provider struct {
	source func() Config
	client *http.Client
}

// NewProvider builds a Provider around a config source.
func NewProvider(source func() Config) *Provider {
	return &Provider{source: source, client: &http.Client{Timeout: 20 * time.Second}}
}

// Enabled reports whether WhatsApp sending is switched on and configured.
func (p *Provider) Enabled() bool { return p.source().Ready() }

// waError is Meta's error envelope.
type waError struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// SendText delivers `text` to `phone` (Indian 10-digit numbers get the default
// country code prefixed). When a template is configured the text travels as its
// single {{1}} body parameter — required for business-initiated messages
// outside WhatsApp's 24-hour customer-service window; free-form text is used
// otherwise (works only inside that window).
func (p *Provider) SendText(ctx context.Context, phone, text string) error {
	cfg := p.source()
	if !cfg.Ready() {
		return fmt.Errorf("whatsapp: not configured")
	}
	to := normalizePhone(phone, cfg.CountryCode)
	if to == "" {
		return fmt.Errorf("whatsapp: invalid phone %q", phone)
	}

	var payload map[string]any
	if t := strings.TrimSpace(cfg.Template); t != "" {
		lang := strings.TrimSpace(cfg.TemplateLang)
		if lang == "" {
			lang = "en"
		}
		payload = map[string]any{
			"messaging_product": "whatsapp",
			"to":                to,
			"type":              "template",
			"template": map[string]any{
				"name":     t,
				"language": map[string]any{"code": lang},
				"components": []map[string]any{{
					"type": "body",
					"parameters": []map[string]any{{
						"type": "text",
						// Template body params may not contain newlines; Meta
						// rejects them. Fold the report onto one line.
						"text": strings.Join(strings.Fields(text), " "),
					}},
				}},
			},
		}
	} else {
		payload = map[string]any{
			"messaging_product": "whatsapp",
			"to":                to,
			"type":              "text",
			"text":              map[string]any{"body": text},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	attempt := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			graphBaseURL+strings.TrimSpace(cfg.PhoneID)+"/messages", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))

		resp, err := p.client.Do(req)
		if err != nil {
			return fmt.Errorf("whatsapp request: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e waError
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			return fmt.Errorf("whatsapp %d (%s): %s", e.Error.Code, e.Error.Type, e.Error.Message)
		}
		return fmt.Errorf("whatsapp status %d: %s", resp.StatusCode, string(raw))
	}

	err = attempt()
	// Meta 133010: the number was added to the account but never REGISTERED
	// for the Cloud API. Auto-register once (default PIN) and retry the send.
	if err != nil && (strings.Contains(err.Error(), "133010") ||
		strings.Contains(strings.ToLower(err.Error()), "register")) {
		if rerr := p.register(ctx, cfg); rerr != nil {
			return fmt.Errorf("%v — auto-register also failed: %v", err, rerr)
		}
		err = attempt()
	}
	return err
}

// normalizePhone strips everything but digits and prefixes the country code
// onto bare national numbers (Meta wants full international format, no "+").
func normalizePhone(phone, countryCode string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	cc := strings.TrimSpace(countryCode)
	if cc == "" {
		cc = "91"
	}
	switch {
	case len(d) == 10:
		return cc + d
	case len(d) >= 11 && len(d) <= 15:
		return d
	default:
		return ""
	}
}

// register performs the one-time Cloud API registration for the phone number
// (Meta error 133010 "not registered"). Uses the default two-step PIN 000000;
// if the owner set a custom PIN in WhatsApp Manager this fails and the real
// error is surfaced to the admin.
func (p *Provider) register(ctx context.Context, cfg Config) error {
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"pin":               "000000",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		graphBaseURL+strings.TrimSpace(cfg.PhoneID)+"/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return fmt.Errorf("whatsapp register %d: %s", e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("whatsapp register status %d: %s", resp.StatusCode, string(raw))
}

// SendOTP delivers a login code via the approved AUTHENTICATION template
// (body {{1}} + copy-code button both carry the code). Auto-registers the
// number once on Meta error 133010, like SendText.
func (p *Provider) SendOTP(ctx context.Context, phone, code string) error {
	cfg := p.source()
	if !cfg.OtpReady() {
		return fmt.Errorf("whatsapp otp: not configured")
	}
	to := normalizePhone(phone, cfg.CountryCode)
	if to == "" {
		return fmt.Errorf("whatsapp otp: invalid phone %q", phone)
	}
	lang := strings.TrimSpace(cfg.OtpLang)
	if lang == "" {
		lang = "en"
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]any{
			"name":     strings.TrimSpace(cfg.OtpTemplate),
			"language": map[string]any{"code": lang},
			"components": []map[string]any{
				{
					"type": "body",
					"parameters": []map[string]any{
						{"type": "text", "text": code},
					},
				},
				{
					"type":     "button",
					"sub_type": "url",
					"index":    "0",
					"parameters": []map[string]any{
						{"type": "text", "text": code},
					},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	attempt := func() error { return p.postMessages(ctx, cfg, body) }
	err = attempt()
	if err != nil && (strings.Contains(err.Error(), "133010") ||
		strings.Contains(strings.ToLower(err.Error()), "register")) {
		if rerr := p.register(ctx, cfg); rerr != nil {
			return fmt.Errorf("%v — auto-register also failed: %v", err, rerr)
		}
		err = attempt()
	}
	return err
}

// postMessages POSTs a prebuilt payload to {phone-id}/messages.
func (p *Provider) postMessages(ctx context.Context, cfg Config, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		graphBaseURL+strings.TrimSpace(cfg.PhoneID)+"/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return fmt.Errorf("whatsapp %d (%s): %s", e.Error.Code, e.Error.Type, e.Error.Message)
	}
	return fmt.Errorf("whatsapp status %d: %s", resp.StatusCode, string(raw))
}
