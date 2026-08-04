package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// GoogleConfig is the admin-managed Google-SSO state exposed to the login page.
type GoogleConfig struct {
	Enabled  bool
	ClientID string
}

// GoogleConfigFunc returns the current config, evaluated per call so admin
// changes apply without a restart.
type GoogleConfigFunc func() GoogleConfig

// GoogleProvider resolves the Google-SSO config from admin settings, with the
// env client id as a fallback for the id (the enable flag comes only from settings).
func GoogleProvider(settings *repository.SettingRepository, envClientID string) GoogleConfigFunc {
	return func() GoogleConfig {
		out := GoogleConfig{ClientID: strings.TrimSpace(envClientID)}
		if s, err := settings.Get(); err == nil {
			out.Enabled = s.GoogleSsoEnabled
			if v := strings.TrimSpace(s.GoogleClientID); v != "" {
				out.ClientID = v
			}
		}
		return out
	}
}

var googleHTTP = &http.Client{Timeout: 10 * time.Second}

// googleClaims is the subset of Google's tokeninfo response we use. tokeninfo
// returns all fields as strings.
type googleClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Aud           string `json:"aud"`
	Iss           string `json:"iss"`
}

// verifyGoogleIDToken validates a Google ID token via Google's tokeninfo
// endpoint (which checks the signature + expiry server-side) and confirms the
// audience matches our OAuth client id and the email is verified.
func verifyGoogleIDToken(ctx context.Context, idToken, clientID string) (*googleClaims, error) {
	if strings.TrimSpace(idToken) == "" {
		return nil, errors.New("missing id_token")
	}
	if strings.TrimSpace(clientID) == "" {
		return nil, errors.New("google sign-in is not configured (set GOOGLE_CLIENT_ID)")
	}
	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(idToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := googleHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google verify: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("invalid or expired Google token")
	}
	var c googleClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	if c.Aud != clientID {
		return nil, errors.New("google token audience mismatch")
	}
	if c.Iss != "accounts.google.com" && c.Iss != "https://accounts.google.com" {
		return nil, errors.New("google token issuer mismatch")
	}
	if c.EmailVerified != "true" {
		return nil, errors.New("google email is not verified")
	}
	if c.Sub == "" {
		return nil, errors.New("google token missing subject")
	}
	return &c, nil
}

// verifyGoogleAccessToken validates a Google OAuth access token via tokeninfo.
// The web google_sign_in flow yields an access token (not an id token), so this
// is the web counterpart of verifyGoogleIDToken. The access-token tokeninfo
// response carries aud/email/email_verified/sub but no iss, so issuer isn't
// checked. It still confirms the token was minted for our OAuth client id.
func verifyGoogleAccessToken(ctx context.Context, accessToken, clientID string) (*googleClaims, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("missing access_token")
	}
	if strings.TrimSpace(clientID) == "" {
		return nil, errors.New("google sign-in is not configured (set GOOGLE_CLIENT_ID)")
	}
	endpoint := "https://oauth2.googleapis.com/tokeninfo?access_token=" + url.QueryEscape(accessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := googleHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google verify: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("invalid or expired Google token")
	}
	var c googleClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	if c.Aud != clientID {
		return nil, errors.New("google token audience mismatch")
	}
	if c.EmailVerified != "true" {
		return nil, errors.New("google email is not verified")
	}
	if c.Sub == "" || c.Email == "" {
		return nil, errors.New("google token missing subject/email")
	}
	// tokeninfo for an access token doesn't return the display name, so fetch it
	// from the OpenID userinfo endpoint (best-effort; empty name isn't fatal —
	// the user can still type it on the profile screen).
	if c.Name == "" {
		if name := googleUserinfoName(ctx, accessToken); name != "" {
			c.Name = name
		}
	}
	return &c, nil
}

// googleUserinfoName fetches the account's display name from the OpenID userinfo
// endpoint using the access token. Returns "" on any failure. This endpoint uses
// the userinfo.profile scope and does not require the People API to be enabled.
func googleUserinfoName(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := googleHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	var info struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return ""
	}
	return strings.TrimSpace(info.Name)
}
