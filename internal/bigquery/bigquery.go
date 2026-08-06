// Package bigquery is a tiny, dependency-free BigQuery REST client: it
// authenticates with a Google service-account JSON (manual RS256 JWT → OAuth2
// token, the same technique as the fcm package) and runs synchronous queries via
// the jobs.query endpoint. Only what the analytics/crash dashboards need.
package bigquery

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
	"strings"
	"sync"
	"time"
)

// bigquery.readonly is enough to run queries and read results.
const scope = "https://www.googleapis.com/auth/bigquery.readonly"

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

// Client runs BigQuery queries for one project using a service account.
type Client struct {
	sa        *serviceAccount
	key       *rsa.PrivateKey
	projectID string
	client    *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// New builds a Client from a service-account JSON string and a project id (the
// GCP project that holds the BigQuery export; falls back to the service
// account's own project when empty). Returns an error on missing/invalid creds.
func New(saJSON, projectID string) (*Client, error) {
	saJSON = strings.TrimSpace(saJSON)
	if saJSON == "" {
		return nil, fmt.Errorf("bigquery: no service account configured")
	}
	var sa serviceAccount
	if err := json.Unmarshal([]byte(saJSON), &sa); err != nil {
		return nil, fmt.Errorf("bigquery: parse credentials: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, fmt.Errorf("bigquery: credentials missing client_email/private_key")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("bigquery: parse private key: %w", err)
	}
	if strings.TrimSpace(projectID) == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("bigquery: no project id")
	}
	return &Client{sa: &sa, key: key, projectID: projectID,
		client: &http.Client{Timeout: 60 * time.Second}}, nil
}

// ProjectID returns the resolved GCP project the client bills/queries under.
func (c *Client) ProjectID() string { return c.projectID }

// Query runs standard-SQL `sql` synchronously and returns the rows as maps keyed
// by column name (all values as strings — BigQuery returns them that way for the
// scalar columns these dashboards use). NULLs become "".
func (c *Client) Query(ctx context.Context, sql string) ([]map[string]string, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	reqBody, _ := json.Marshal(map[string]any{
		"query":        sql,
		"useLegacySql": false,
		"timeoutMs":    55000,
	})
	endpoint := fmt.Sprintf("https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries",
		url.PathEscape(c.projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bigquery request: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("bigquery %s: %s", res.Status, truncate(raw, 400))
	}

	var out struct {
		Schema struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"schema"`
		Rows []struct {
			F []struct {
				V json.RawMessage `json:"v"`
			} `json:"f"`
		} `json:"rows"`
		JobComplete bool `json:"jobComplete"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bigquery decode: %w", err)
	}

	names := make([]string, len(out.Schema.Fields))
	for i, f := range out.Schema.Fields {
		names[i] = f.Name
	}
	rows := make([]map[string]string, 0, len(out.Rows))
	for _, r := range out.Rows {
		m := make(map[string]string, len(r.F))
		for i, cell := range r.F {
			if i >= len(names) {
				break
			}
			m[names[i]] = scalar(cell.V)
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// scalar coerces a BigQuery cell value to a string ("" for null).
func scalar(v json.RawMessage) string {
	if len(v) == 0 || string(v) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	return strings.Trim(string(v), `"`)
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-30*time.Second)) {
		return c.token, nil
	}
	jwt, err := c.signedJWT()
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("bigquery token: %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	c.token = out.AccessToken
	c.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) signedJWT() (string, error) {
	now := time.Now()
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss":   c.sa.ClientEmail,
		"scope": scope,
		"aud":   c.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signingInput := header + "." + b64(claims)
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, h[:])
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

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
