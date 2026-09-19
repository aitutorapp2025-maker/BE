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
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const graphBaseURL = "https://graph.facebook.com/v20.0/"

// Config is the live WhatsApp configuration (admin Settings win over env).
type Config struct {
	Enabled      bool
	Token        string // permanent access token (System User)
	PhoneID      string // WhatsApp Business phone number ID
	WABAID       string // WhatsApp Business Account ID (to list message templates)
	AppID        string // Meta App ID (for the resumable sample-image upload when creating templates)
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

// ── Broadcast campaigns (templates + image header + bulk send) ───────────────

// TemplateMessage is one approved-template send for a campaign: an image header
// (by uploaded Meta media id, or a public link) plus ordered body parameters
// ({{1}}, {{2}}, …). Either HeaderImageID or HeaderImageLink may be set; ID wins.
type TemplateMessage struct {
	Phone           string
	Name            string   // approved template name
	Lang            string   // language code (default "en")
	HeaderImageID   string   // Meta media id (preferred — upload once, reuse)
	HeaderImageLink string   // OR a public https image URL
	BodyParams      []string // ordered values for {{1}}, {{2}}, …
}

// SendTemplate delivers an approved template message (optional image header +
// body params) and returns Meta's message id (wamid) on success. Auto-registers
// the number once on Meta error 133010, like SendText. This is the campaign path.
func (p *Provider) SendTemplate(ctx context.Context, m TemplateMessage) (string, error) {
	cfg := p.source()
	if !cfg.Ready() {
		return "", fmt.Errorf("whatsapp: not configured")
	}
	to := normalizePhone(m.Phone, cfg.CountryCode)
	if to == "" {
		return "", fmt.Errorf("whatsapp: invalid phone %q", m.Phone)
	}
	lang := strings.TrimSpace(m.Lang)
	if lang == "" {
		lang = "en"
	}

	var components []map[string]any
	// Image header component (id preferred; else public link).
	if id := strings.TrimSpace(m.HeaderImageID); id != "" {
		components = append(components, map[string]any{
			"type":       "header",
			"parameters": []map[string]any{{"type": "image", "image": map[string]any{"id": id}}},
		})
	} else if link := strings.TrimSpace(m.HeaderImageLink); link != "" {
		components = append(components, map[string]any{
			"type":       "header",
			"parameters": []map[string]any{{"type": "image", "image": map[string]any{"link": link}}},
		})
	}
	// Body parameters. Meta rejects newlines in a param, so fold whitespace.
	if len(m.BodyParams) > 0 {
		params := make([]map[string]any, 0, len(m.BodyParams))
		for _, v := range m.BodyParams {
			params = append(params, map[string]any{
				"type": "text",
				"text": strings.Join(strings.Fields(v), " "),
			})
		}
		components = append(components, map[string]any{"type": "body", "parameters": params})
	}

	tpl := map[string]any{
		"name":     strings.TrimSpace(m.Name),
		"language": map[string]any{"code": lang},
	}
	if len(components) > 0 {
		tpl["components"] = components
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template":          tpl,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id, err := p.postMessagesID(ctx, cfg, body)
	if err != nil && (strings.Contains(err.Error(), "133010") ||
		strings.Contains(strings.ToLower(err.Error()), "register")) {
		if rerr := p.register(ctx, cfg); rerr == nil {
			id, err = p.postMessagesID(ctx, cfg, body)
		}
	}
	return id, err
}

// postMessagesID is like postMessages but returns Meta's message id (wamid) so
// the campaign can store it per recipient for delivery tracking.
func (p *Provider) postMessagesID(ctx context.Context, cfg Config, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		graphBaseURL+strings.TrimSpace(cfg.PhoneID)+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whatsapp request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &ok)
		if len(ok.Messages) > 0 {
			return ok.Messages[0].ID, nil
		}
		return "", nil
	}
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return "", fmt.Errorf("whatsapp %d (%s): %s", e.Error.Code, e.Error.Type, e.Error.Message)
	}
	return "", fmt.Errorf("whatsapp status %d: %s", resp.StatusCode, string(raw))
}

// TemplateButton is one template button. Type is URL | PHONE_NUMBER |
// QUICK_REPLY. URL carries a link (static "Click here" style); PHONE_NUMBER a
// call number; QUICK_REPLY just a tappable reply with Text.
type TemplateButton struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`
	Phone string `json:"phone_number,omitempty"`
}

// buttonsComponent builds the Meta BUTTONS component (nil when there are none).
func buttonsComponent(btns []TemplateButton) map[string]any {
	arr := make([]map[string]any, 0, len(btns))
	for _, b := range btns {
		t := strings.ToUpper(strings.TrimSpace(b.Type))
		if t == "" || strings.TrimSpace(b.Text) == "" {
			continue
		}
		m := map[string]any{"type": t, "text": strings.TrimSpace(b.Text)}
		switch t {
		case "URL":
			m["url"] = strings.TrimSpace(b.URL)
		case "PHONE_NUMBER":
			m["phone_number"] = strings.TrimSpace(b.Phone)
		}
		arr = append(arr, m)
	}
	if len(arr) == 0 {
		return nil
	}
	return map[string]any{"type": "BUTTONS", "buttons": arr}
}

// Template is a simplified approved WhatsApp template for the campaign picker.
type Template struct {
	ID             string `json:"id"` // Meta template id (needed to edit)
	Name           string `json:"name"`
	Language       string `json:"language"`
	Status         string `json:"status"`          // APPROVED, PENDING, REJECTED
	Category       string `json:"category"`        // MARKETING, UTILITY, AUTHENTICATION
	HeaderFormat   string `json:"header_format"`   // TEXT | IMAGE | VIDEO | DOCUMENT | ""
	BodyText       string           `json:"body_text"`
	BodyParams     int              `json:"body_params"`     // count of {{n}} in the body
	RejectedReason string           `json:"rejected_reason"` // why Meta rejected (when status REJECTED)
	Buttons        []TemplateButton `json:"buttons"`
}

// ListTemplates fetches the WABA's message templates (GET
// /{WABA_ID}/message_templates). Requires the WABA id + token in Settings.
func (p *Provider) ListTemplates(ctx context.Context) ([]Template, error) {
	cfg := p.source()
	waba := strings.TrimSpace(cfg.WABAID)
	if strings.TrimSpace(cfg.Token) == "" || waba == "" {
		return nil, fmt.Errorf("whatsapp: set the WABA id + access token in Settings first")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		graphBaseURL+waba+"/message_templates?limit=200&fields=id,name,language,status,category,components,quality_score,rejected_reason", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whatsapp templates: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e waError
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			return nil, fmt.Errorf("whatsapp templates %d: %s", e.Error.Code, e.Error.Message)
		}
		return nil, fmt.Errorf("whatsapp templates status %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Language       string `json:"language"`
			Status         string `json:"status"`
			Category       string `json:"category"`
			RejectedReason string `json:"rejected_reason"`
			Components     []struct {
				Type    string `json:"type"`
				Format  string `json:"format"`
				Text    string `json:"text"`
				Buttons []struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					URL         string `json:"url"`
					PhoneNumber string `json:"phone_number"`
				} `json:"buttons"`
			} `json:"components"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	tpls := make([]Template, 0, len(out.Data))
	for _, d := range out.Data {
		t := Template{ID: d.ID, Name: d.Name, Language: d.Language, Status: d.Status,
			Category: d.Category, RejectedReason: d.RejectedReason}
		for _, c := range d.Components {
			switch strings.ToUpper(c.Type) {
			case "HEADER":
				t.HeaderFormat = strings.ToUpper(c.Format)
			case "BODY":
				t.BodyText = c.Text
				t.BodyParams = countPlaceholders(c.Text)
			case "BUTTONS":
				for _, b := range c.Buttons {
					t.Buttons = append(t.Buttons, TemplateButton{
						Type: strings.ToUpper(b.Type), Text: b.Text,
						URL: b.URL, Phone: b.PhoneNumber,
					})
				}
			}
		}
		tpls = append(tpls, t)
	}
	return tpls, nil
}

// countPlaceholders returns the highest {{n}} index in a template body (Meta
// numbers body params sequentially from 1, so the max index is the param count).
func countPlaceholders(s string) int {
	max := 0
	for i := 1; i <= 30; i++ {
		if strings.Contains(s, fmt.Sprintf("{{%d}}", i)) {
			max = i
		}
	}
	return max
}

// CreateTemplateInput describes a template to submit to Meta for approval
// (v1 supports: IMAGE header + BODY with {{n}} variables + optional FOOTER).
type CreateTemplateInput struct {
	Name         string   // lowercase + underscores, e.g. "welcome_greeting"
	Language     string   // e.g. "en_US" (default), "en", "ta"
	Category     string   // MARKETING (default) | UTILITY
	BodyText     string   // may contain {{1}}, {{2}}, …
	BodyExamples []string // one sample value per variable, in order (required if body has vars)
	Footer       string   // optional
	HeaderImage  []byte   // sample header image bytes (optional)
	HeaderMime   string   // e.g. "image/jpeg", "image/png"
	Buttons      []TemplateButton // optional URL / call / quick-reply buttons
}

// CreateTemplate submits a new image-header template to Meta for approval. It
// first uploads the sample header image via the Resumable Upload API to obtain a
// header_handle, then POSTs the template to /{WABA_ID}/message_templates.
// Returns the new template id + status (usually "PENDING"). Requires the WABA id,
// App id and a token with the whatsapp_business_management scope.
func (p *Provider) CreateTemplate(ctx context.Context, in CreateTemplateInput) (id, status string, err error) {
	cfg := p.source()
	waba := strings.TrimSpace(cfg.WABAID)
	if strings.TrimSpace(cfg.Token) == "" || waba == "" {
		return "", "", fmt.Errorf("whatsapp: set the WABA id + access token in Settings first")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", fmt.Errorf("template name is required")
	}
	lang := strings.TrimSpace(in.Language)
	if lang == "" {
		lang = "en_US"
	}
	cat := strings.ToUpper(strings.TrimSpace(in.Category))
	if cat == "" {
		cat = "MARKETING"
	}

	var components []map[string]any
	// Optional IMAGE header — included only when a sample image was provided.
	// Without it the template is text-only (body + optional footer).
	if len(in.HeaderImage) > 0 {
		handle, err := p.uploadSampleHeader(ctx, cfg, in.HeaderImage, strings.TrimSpace(in.HeaderMime))
		if err != nil {
			return "", "", err
		}
		components = append(components, map[string]any{
			"type":    "HEADER",
			"format":  "IMAGE",
			"example": map[string]any{"header_handle": []string{handle}},
		})
	}
	bodyComp := map[string]any{"type": "BODY", "text": in.BodyText}
	if len(in.BodyExamples) > 0 {
		// Meta expects body_text as an array-of-arrays: [[ "v1","v2" ]].
		bodyComp["example"] = map[string]any{"body_text": [][]string{in.BodyExamples}}
	}
	components = append(components, bodyComp)
	if f := strings.TrimSpace(in.Footer); f != "" {
		components = append(components, map[string]any{"type": "FOOTER", "text": f})
	}
	if comp := buttonsComponent(in.Buttons); comp != nil {
		components = append(components, comp)
	}

	payload := map[string]any{
		"name":       name,
		"language":   lang,
		"category":   cat,
		"components": components,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		graphBaseURL+waba+"/message_templates", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("create template: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e waError
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			return "", "", fmt.Errorf("create template %d: %s", e.Error.Code, e.Error.Message)
		}
		return "", "", fmt.Errorf("create template status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Category string `json:"category"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.ID, out.Status, nil
}

// uploadSampleHeader uploads the sample header image via Meta's Resumable Upload
// API and returns the header_handle to embed in a template's HEADER example.
// Two steps: (1) start a session at /{APP_ID}/uploads, (2) POST the bytes to the
// returned session id (note the "OAuth" auth scheme + file_offset header).
func (p *Provider) uploadSampleHeader(ctx context.Context, cfg Config, data []byte, mimeType string) (string, error) {
	appID := strings.TrimSpace(cfg.AppID)
	if appID == "" {
		return "", fmt.Errorf("whatsapp: set the Meta App ID in Settings to create image templates")
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	// 1. Start the upload session.
	startURL := fmt.Sprintf("%s%s/uploads?file_length=%d&file_type=%s",
		graphBaseURL, appID, len(data), url.QueryEscape(mimeType))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload session: %w", err)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e waError
		if json.Unmarshal(raw, &e) == nil && e.Error != nil {
			return "", fmt.Errorf("upload session %d: %s", e.Error.Code, e.Error.Message)
		}
		return "", fmt.Errorf("upload session status %d: %s", resp.StatusCode, string(raw))
	}
	var sess struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &sess) != nil || sess.ID == "" {
		return "", fmt.Errorf("upload session: no id returned")
	}
	// 2. Upload the bytes to the session (OAuth scheme + file_offset are required).
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, graphBaseURL+sess.ID, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req2.Header.Set("Authorization", "OAuth "+strings.TrimSpace(cfg.Token))
	req2.Header.Set("file_offset", "0")
	req2.Header.Set("Content-Type", mimeType)
	resp2, err := p.client.Do(req2)
	if err != nil {
		return "", fmt.Errorf("upload bytes: %w", err)
	}
	raw2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
	resp2.Body.Close()
	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		var e waError
		if json.Unmarshal(raw2, &e) == nil && e.Error != nil {
			return "", fmt.Errorf("upload bytes %d: %s", e.Error.Code, e.Error.Message)
		}
		return "", fmt.Errorf("upload bytes status %d: %s", resp2.StatusCode, string(raw2))
	}
	var h struct {
		H string `json:"h"`
	}
	if json.Unmarshal(raw2, &h) != nil || h.H == "" {
		return "", fmt.Errorf("upload bytes: no handle returned")
	}
	return h.H, nil
}

// EditTemplateInput describes changes to an existing template. HeaderImage is
// optional — provide it only to change the image (a fresh sample handle is
// uploaded); omit it to leave the current header untouched.
type EditTemplateInput struct {
	MetaID       string // Meta template id (from ListTemplates)
	Category     string // optional; Meta restricts category changes
	BodyText     string
	BodyExamples []string
	Footer       string
	HeaderImage  []byte
	HeaderMime   string
	Buttons      []TemplateButton
}

// EditTemplate updates an existing template (POST /{TEMPLATE_ID}). Meta only
// allows editing APPROVED/REJECTED/PAUSED templates (not PENDING), rate-limits
// edits, and sends an edited APPROVED template back to PENDING for re-approval.
func (p *Provider) EditTemplate(ctx context.Context, in EditTemplateInput) error {
	cfg := p.source()
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("whatsapp: access token required")
	}
	id := strings.TrimSpace(in.MetaID)
	if id == "" {
		return fmt.Errorf("template id required to edit")
	}
	var components []map[string]any
	if len(in.HeaderImage) > 0 {
		handle, err := p.uploadSampleHeader(ctx, cfg, in.HeaderImage, strings.TrimSpace(in.HeaderMime))
		if err != nil {
			return err
		}
		components = append(components, map[string]any{
			"type":    "HEADER",
			"format":  "IMAGE",
			"example": map[string]any{"header_handle": []string{handle}},
		})
	}
	if strings.TrimSpace(in.BodyText) != "" {
		bodyComp := map[string]any{"type": "BODY", "text": in.BodyText}
		if len(in.BodyExamples) > 0 {
			bodyComp["example"] = map[string]any{"body_text": [][]string{in.BodyExamples}}
		}
		components = append(components, bodyComp)
	}
	if f := strings.TrimSpace(in.Footer); f != "" {
		components = append(components, map[string]any{"type": "FOOTER", "text": f})
	}
	if comp := buttonsComponent(in.Buttons); comp != nil {
		components = append(components, comp)
	}
	payload := map[string]any{}
	if len(components) > 0 {
		payload["components"] = components
	}
	if c := strings.ToUpper(strings.TrimSpace(in.Category)); c != "" {
		payload["category"] = c
	}
	if len(payload) == 0 {
		return fmt.Errorf("nothing to update")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphBaseURL+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("edit template: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return fmt.Errorf("edit template %d: %s", e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("edit template status %d: %s", resp.StatusCode, string(raw))
}

// DeleteTemplate removes a template by name (all languages) from the WABA.
func (p *Provider) DeleteTemplate(ctx context.Context, name string) error {
	cfg := p.source()
	waba := strings.TrimSpace(cfg.WABAID)
	if strings.TrimSpace(cfg.Token) == "" || waba == "" {
		return fmt.Errorf("whatsapp: WABA id + access token required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("template name required")
	}
	u := graphBaseURL + waba + "/message_templates?name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return fmt.Errorf("delete template %d: %s", e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("delete template status %d: %s", resp.StatusCode, string(raw))
}

// UploadMedia uploads an image to Meta and returns its media id, reusable as the
// image header for every recipient in a campaign (upload once, reference many).
// Works even when our own server isn't publicly reachable (unlike a link header).
// mimeType is e.g. "image/jpeg" or "image/png".
func (p *Provider) UploadMedia(ctx context.Context, data []byte, filename, mimeType string) (string, error) {
	cfg := p.source()
	if !cfg.Ready() {
		return "", fmt.Errorf("whatsapp: not configured")
	}
	// Detect the true type from the bytes so the declared type always matches
	// the content (a mismatch also makes Meta reject the upload).
	if sniff := http.DetectContentType(data); strings.HasPrefix(sniff, "image/") {
		mimeType = sniff
	} else if strings.TrimSpace(mimeType) == "" {
		mimeType = "image/jpeg"
	}
	// Give the file a name whose extension matches the type; some Graph checks
	// look at the extension too.
	fname := "upload.jpg"
	switch mimeType {
	case "image/png":
		fname = "upload.png"
	case "image/webp":
		fname = "upload.webp"
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("messaging_product", "whatsapp")
	_ = w.WriteField("type", mimeType)
	// IMPORTANT: set the file PART's Content-Type. CreateFormFile defaults to
	// application/octet-stream, which Meta rejects with error #100.
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename=%q`, fname))
	hdr.Set("Content-Type", mimeType)
	fw, err := w.CreatePart(hdr)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		graphBaseURL+strings.TrimSpace(cfg.PhoneID)+"/media", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whatsapp media: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &ok) == nil && ok.ID != "" {
			return ok.ID, nil
		}
		return "", fmt.Errorf("whatsapp media: no id in response")
	}
	var e waError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		return "", fmt.Errorf("whatsapp media %d: %s", e.Error.Code, e.Error.Message)
	}
	return "", fmt.Errorf("whatsapp media status %d: %s", resp.StatusCode, string(raw))
}
