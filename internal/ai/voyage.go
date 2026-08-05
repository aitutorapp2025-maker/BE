package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/redis/go-redis/v9"
)

const voyageURL = "https://api.voyageai.com/v1/embeddings"

// queryCacheTTL is how long a cached query embedding is kept. The same query
// text always maps to the same vector, so repeated questions skip the embedder.
const queryCacheTTL = 30 * 24 * time.Hour

// Embedder turns text into vectors — either via the Voyage AI cloud API or a
// self-hosted BGE-M3 endpoint (config.EmbedProvider). Query embeddings are cached
// in Redis so repeated questions don't re-embed. Config is read per call so
// admin-panel changes take effect without a restart.
type Embedder struct {
	cfg    config.AIConfigFunc
	client *http.Client
	rdb    *redis.Client // optional query-embedding cache (nil = no cache)
}

// NewEmbedder builds an Embedder. rdb (optional) caches query embeddings.
func NewEmbedder(cfg config.AIConfigFunc, rdb *redis.Client) *Embedder {
	return &Embedder{cfg: cfg, client: apiClient(60 * time.Second), rdb: rdb}
}

type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"` // "document" | "query"
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// EmbedDocuments embeds a batch of passages for storage (input_type "document").
func (e *Embedder) EmbedDocuments(ctx context.Context, texts []string) ([]Vector, error) {
	return e.embed(ctx, texts, "document")
}

// EmbedQuery embeds a single search query (input_type "query"), served from the
// Redis cache when the same query was embedded before.
func (e *Embedder) EmbedQuery(ctx context.Context, text string) (Vector, error) {
	cfg := e.cfg()
	key := e.cacheKey(cfg, text)
	if v, ok := e.cacheGet(ctx, key); ok {
		return v, nil
	}
	vecs, err := e.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embeddings: empty response")
	}
	e.cacheSet(ctx, key, vecs[0])
	return vecs[0], nil
}

// embed dispatches to the local BGE-M3 endpoint or the Voyage cloud API. The
// input type ("document"/"query") is only used by Voyage.
func (e *Embedder) embed(ctx context.Context, texts []string, inputType string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	cfg := e.cfg()
	if cfg.EmbedProvider == "local" {
		return e.embedLocal(ctx, cfg.LocalEmbedURL, cfg.LocalEmbedModel, texts)
	}
	if cfg.VoyageKey == "" {
		return nil, fmt.Errorf("voyage: no API key configured (set it in admin Settings)")
	}
	body, err := json.Marshal(voyageRequest{Input: texts, Model: cfg.VoyageModel, InputType: inputType})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.VoyageKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var out voyageResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("voyage decode: %w", err)
	}
	vecs := make([]Vector, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = Vector(d.Embedding)
	}
	return vecs, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// cacheKey scopes the cache by provider + model so switching embedders never
// serves a stale vector from the wrong model.
func (e *Embedder) cacheKey(cfg config.AIConfig, text string) string {
	provider := cfg.EmbedProvider
	model := cfg.VoyageModel
	if provider == "local" {
		model = cfg.LocalEmbedModel
	}
	sum := sha256.Sum256([]byte(provider + "|" + model + "|" + text))
	return "emb:" + hex.EncodeToString(sum[:])
}

func (e *Embedder) cacheGet(ctx context.Context, key string) (Vector, bool) {
	if e.rdb == nil {
		return nil, false
	}
	raw, err := e.rdb.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var v Vector
	if json.Unmarshal(raw, &v) != nil || len(v) == 0 {
		return nil, false
	}
	return v, true
}

func (e *Embedder) cacheSet(ctx context.Context, key string, v Vector) {
	if e.rdb == nil || len(v) == 0 {
		return
	}
	if raw, err := json.Marshal(v); err == nil {
		_ = e.rdb.Set(ctx, key, raw, queryCacheTTL).Err()
	}
}
