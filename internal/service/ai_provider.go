package service

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// AIProvider returns a config.AIConfigFunc that reads the AI keys + model names
// from the DB (admin settings), falling back to the environment. Keys set in the
// admin panel win; the embedding dimension and top-K stay environment-only
// (they're tied to the vector column / infra, not something the admin tunes).
//
// Evaluated per call, so changing the keys in the admin panel takes effect on
// the next request without a restart.
func AIProvider(settings *repository.SettingRepository, envFallback config.AIConfig) config.AIConfigFunc {
	return func() config.AIConfig {
		out := envFallback // carries EmbedDim + TopK + env-provided defaults
		s, err := settings.Get()
		if err != nil {
			return out
		}
		if k := strings.TrimSpace(s.AnthropicAPIKey); k != "" {
			out.AnthropicKey = k
		}
		if m := strings.TrimSpace(s.AnthropicModel); m != "" {
			out.AnthropicModel = m
		}
		if k := strings.TrimSpace(s.VoyageAPIKey); k != "" {
			out.VoyageKey = k
		}
		if m := strings.TrimSpace(s.VoyageModel); m != "" {
			out.VoyageModel = m
		}
		// Embeddings backend (admin-toggleable): voyage | local.
		if p := strings.TrimSpace(s.EmbeddingsProvider); p != "" {
			out.EmbedProvider = p
		}
		if u := strings.TrimSpace(s.LocalEmbedURL); u != "" {
			out.LocalEmbedURL = u
		}
		if m := strings.TrimSpace(s.LocalEmbedModel); m != "" {
			out.LocalEmbedModel = m
		}
		// The admin "AI enabled" toggle only matters when keys exist; the
		// pipeline is usable whenever both keys are present (DB or env).
		if !s.AIEnabled && strings.TrimSpace(s.AnthropicAPIKey) == "" && strings.TrimSpace(s.VoyageAPIKey) == "" {
			// nothing configured in DB — pure env fallback already in `out`
		}
		return out
	}
}
