package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// localEmbedRequest matches the Ollama batch embeddings API (/api/embed), which
// also serves the BGE-M3 model (`ollama pull bge-m3`). Text-Embeddings-Inference
// (TEI) and other servers use a slightly different shape — point LOCAL_EMBED_URL
// at an Ollama-compatible endpoint, or add an adapter here.
type localEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type localEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// embedLocal calls the self-hosted embeddings server (e.g. Ollama running
// bge-m3) and returns one vector per input text.
func (e *Embedder) embedLocal(ctx context.Context, url, model string, texts []string) ([]Vector, error) {
	if url == "" {
		return nil, fmt.Errorf("local embeddings: no LOCAL_EMBED_URL configured")
	}
	body, err := json.Marshal(localEmbedRequest{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local embeddings request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local embeddings status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var out localEmbedResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("local embeddings decode: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("local embeddings: got %d vectors for %d inputs", len(out.Embeddings), len(texts))
	}
	vecs := make([]Vector, len(out.Embeddings))
	for i, v := range out.Embeddings {
		vecs[i] = Vector(v)
	}
	return vecs, nil
}
