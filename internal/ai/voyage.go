package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
)

const voyageURL = "https://api.voyageai.com/v1/embeddings"

// Embedder turns text into vectors via the Voyage AI embeddings API. Voyage has
// no official Go SDK, so it's called over net/http. The key + model are read per
// call from the config func, so admin-panel changes take effect without a
// restart.
type Embedder struct {
	cfg    config.AIConfigFunc
	client *http.Client
}

// NewEmbedder builds an Embedder that reads its key + model from cfg per call.
func NewEmbedder(cfg config.AIConfigFunc) *Embedder {
	return &Embedder{cfg: cfg, client: &http.Client{Timeout: 60 * time.Second}}
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

// EmbedQuery embeds a single search query (input_type "query").
func (e *Embedder) EmbedQuery(ctx context.Context, text string) (Vector, error) {
	vecs, err := e.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("voyage: empty embedding response")
	}
	return vecs[0], nil
}

func (e *Embedder) embed(ctx context.Context, texts []string, inputType string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	cfg := e.cfg()
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
