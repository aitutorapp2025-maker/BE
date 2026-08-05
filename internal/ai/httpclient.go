package ai

import (
	"net/http"
	"time"
)

// apiClient returns an *http.Client tuned for talking to a single external API
// host (Anthropic / Voyage). Go's default transport caps idle connections per
// host at 2, so bursts of concurrent AI calls reopen TLS connections instead of
// reusing warm ones; a generous idle pool avoids that handshake cost.
func apiClient(timeout time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 100
	t.IdleConnTimeout = 90 * time.Second
	return &http.Client{Timeout: timeout, Transport: t}
}
