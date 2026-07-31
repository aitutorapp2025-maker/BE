// Package fcm sends push notifications via the Firebase Cloud Messaging HTTP v1
// API using a Google service account — stdlib only (manual RS256 JWT → OAuth2
// access token → messages:send), so it needs no extra dependencies.
//
// Configure with a service-account JSON via one of:
//   FCM_CREDENTIALS_FILE=/path/to/service-account.json
//   FCM_CREDENTIALS_JSON={...}   (the JSON inline)
// When neither is set, the sender is disabled (Enabled() == false) and Send is a
// no-op, so the rest of the app runs normally until credentials are added.
package fcm

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const scope = "https://www.googleapis.com/auth/firebase.messaging"

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

// Sender delivers FCM messages. The zero value / a nil Sender is disabled.
type Sender struct {
	sa     *serviceAccount
	key    *rsa.PrivateKey
	client *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewFromEnv builds a Sender from FCM_CREDENTIALS_FILE / FCM_CREDENTIALS_JSON.
// Returns a disabled sender (no error) when unconfigured; returns an error only
// when credentials are present but malformed.
func NewFromEnv() (*Sender, error) {
	raw := strings.TrimSpace(os.Getenv("FCM_CREDENTIALS_JSON"))
	if raw == "" {
		if f := strings.TrimSpace(os.Getenv("FCM_CREDENTIALS_FILE")); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, fmt.Errorf("fcm: read credentials file: %w", err)
			}
			raw = string(b)
		}
	}
	if raw == "" {
		return &Sender{}, nil // disabled
	}
	var sa serviceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return nil, fmt.Errorf("fcm: parse credentials: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" || sa.ProjectID == "" {
		return nil, fmt.Errorf("fcm: credentials missing client_email/private_key/project_id")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("fcm: parse private key: %w", err)
	}
	return &Sender{sa: &sa, key: key, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

// Enabled reports whether credentials are configured.
func (s *Sender) Enabled() bool { return s != nil && s.sa != nil && s.key != nil }

// SendToTokens sends the same notification to each device token, returning how
// many were accepted. Individual failures are collected, not fatal.
func (s *Sender) SendToTokens(ctx context.Context, tokens []string, title, body string, data map[string]string) (int, error) {
	if !s.Enabled() {
		return 0, fmt.Errorf("fcm: not configured")
	}
	tok, err := s.accessToken(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	var firstErr error
	for _, t := range tokens {
		if strings.TrimSpace(t) == "" {
			continue
		}
		if err := s.send(ctx, tok, t, title, body, data); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent++
	}
	return sent, firstErr
}

func (s *Sender) send(ctx context.Context, accessToken, deviceToken, title, body string, data map[string]string) error {
	msg := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]string{
				"title": title,
				"body":  body,
			},
		},
	}
	if len(data) > 0 {
		msg["message"].(map[string]any)["data"] = data
	}
	payload, _ := json.Marshal(msg)
	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", s.sa.ProjectID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("fcm send: %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// accessToken returns a cached OAuth2 token, minting a new one when near expiry.
func (s *Sender) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExp.Add(-30*time.Second)) {
		return s.token, nil
	}
	jwt, err := s.signedJWT()
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.sa.TokenURI,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("fcm token: %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	s.token = out.AccessToken
	s.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return s.token, nil
}

func (s *Sender) signedJWT() (string, error) {
	now := time.Now()
	header := b64(`{"alg":"RS256","typ":"JWT"}`)
	claims, _ := json.Marshal(map[string]any{
		"iss":   s.sa.ClientEmail,
		"scope": scope,
		"aud":   s.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signingInput := header + "." + b64Bytes(claims)
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, fmt.Errorf("not an RSA key")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func b64(s string) string      { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func b64Bytes(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
