package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
)

// Gemini (Google) is the alternative answers provider — much cheaper per call
// than Claude for the high-volume tutor chat. Same plain-net/http style as the
// Claude client; the admin picks the provider + key in Settings → AI Tutor.
const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models/"

// geminiPart is one piece of a turn: text, or inline media (image/PDF).
type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiGenConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// geminiText concatenates the text parts of the first candidate.
func (r *geminiResponse) text() string {
	var b strings.Builder
	if len(r.Candidates) > 0 {
		for _, p := range r.Candidates[0].Content.Parts {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func geminiReq(cfg config.AIConfig, system string, user []geminiPart, maxTokens int) geminiRequest {
	req := geminiRequest{
		Contents:         []geminiContent{{Role: "user", Parts: user}},
		GenerationConfig: geminiGenConfig{MaxOutputTokens: maxTokens},
	}
	if system != "" {
		req.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: system}}}
	}
	return req
}

// TranscribeAudio turns a student's voice note into text using Gemini's audio
// understanding (Claude has no audio input, so this runs on the Gemini key
// whenever one is configured — regardless of which provider answers the
// tutor). langHint biases the transcription ("Tamil", "English", ...).
func (c *Chat) TranscribeAudio(ctx context.Context, audioB64, mimeType, langHint string) (string, error) {
	if c.cfg().GeminiKey == "" {
		return "", fmt.Errorf("voice transcription needs the Gemini API key — add it in admin Settings → AI Tutor")
	}
	prompt := "Transcribe this audio EXACTLY as spoken, in the original spoken language. " +
		"Return ONLY the transcription text — no labels, no translation, no commentary."
	if strings.TrimSpace(langHint) != "" {
		prompt += " The speaker is most likely speaking " + strings.TrimSpace(langHint) + "."
	}
	out, err := c.sendGemini(ctx, "", []geminiPart{
		{InlineData: &geminiInlineData{MimeType: mimeType, Data: audioB64}},
		{Text: prompt},
	}, 1500)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// sendGemini performs one non-streaming generateContent call.
func (c *Chat) sendGemini(ctx context.Context, system string, user []geminiPart, maxTokens int) (string, error) {
	cfg := c.cfg()
	if cfg.GeminiKey == "" {
		return "", fmt.Errorf("gemini: no API key configured (set it in admin Settings)")
	}
	body, err := json.Marshal(geminiReq(cfg, system, user, maxTokens))
	if err != nil {
		return "", err
	}
	url := geminiBaseURL + cfg.GeminiModel + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", cfg.GeminiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out geminiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("gemini decode (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("gemini %s: %s", out.Error.Status, out.Error.Message)
		}
		return "", fmt.Errorf("gemini status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	return out.text(), nil
}

// streamGemini streams the answer via streamGenerateContent (SSE), invoking
// onDelta per text chunk, and returns the full concatenated text.
func (c *Chat) streamGemini(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
	cfg := c.cfg()
	if cfg.GeminiKey == "" {
		return "", fmt.Errorf("gemini: no API key configured (set it in admin Settings)")
	}
	body, err := json.Marshal(geminiReq(cfg, system, []geminiPart{{Text: user}}, 1500))
	if err != nil {
		return "", err
	}
	url := geminiBaseURL + cfg.GeminiModel + ":streamGenerateContent?alt=sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", cfg.GeminiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var out geminiResponse
		if json.Unmarshal(raw, &out) == nil && out.Error != nil {
			return "", fmt.Errorf("gemini %s: %s", out.Error.Status, out.Error.Message)
		}
		return "", fmt.Errorf("gemini status %d: %s", resp.StatusCode, truncate(raw, 300))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var full strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk geminiResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return full.String(), fmt.Errorf("gemini %s: %s", chunk.Error.Status, chunk.Error.Message)
		}
		if t := chunk.text(); t != "" {
			full.WriteString(t)
			if onDelta != nil {
				onDelta(t)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), fmt.Errorf("gemini stream read: %w", err)
	}
	return full.String(), nil
}
