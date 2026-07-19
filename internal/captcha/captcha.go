// Package captcha verifies a CAPTCHA token with the provider's siteverify
// endpoint. Supports Google reCAPTCHA, hCaptcha and Cloudflare Turnstile — they
// all accept a POST of {secret, response} and return {"success": bool}.
package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Providers.
const (
	ProviderReCaptcha = "recaptcha"
	ProviderHCaptcha  = "hcaptcha"
	ProviderTurnstile = "turnstile"
)

func verifyURL(provider string) string {
	switch provider {
	case ProviderHCaptcha:
		return "https://hcaptcha.com/siteverify"
	case ProviderTurnstile:
		return "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	default:
		return "https://www.google.com/recaptcha/api/siteverify"
	}
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Verify checks a token against the provider. Returns nil if the token is valid.
func Verify(ctx context.Context, provider, secret, token, remoteIP string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing captcha token")
	}

	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		verifyURL(provider), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("captcha verify request: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("captcha verify response: %w", err)
	}
	if !out.Success {
		return fmt.Errorf("captcha rejected: %s", strings.Join(out.ErrorCodes, ","))
	}
	return nil
}
