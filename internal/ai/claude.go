package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
)

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
)

// Chat generates grounded tutor answers via the Claude Messages API. Called over
// net/http to avoid a new module dependency; the request shape follows the
// current Messages API (system + user turns, adaptive thinking left off for
// low-latency student answers). Key + model are read per call from the config
// func so admin-panel changes take effect without a restart.
type Chat struct {
	cfg    config.AIConfigFunc
	client *http.Client
}

// NewChat builds a Chat client that reads its key + model from cfg per call.
func NewChat(cfg config.AIConfigFunc) *Chat {
	return &Chat{cfg: cfg, client: apiClient(90 * time.Second)}
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role string `json:"role"`
	// Content is a plain string for text-only turns, or a slice of content blocks
	// (see contentBlock) when an image is attached (vision).
	Content any `json:"content"`
}

// contentBlock is one part of a multimodal user turn (text or image).
type contentBlock struct {
	Type   string       `json:"type"`             // "text" | "image"
	Text   string       `json:"text,omitempty"`   // for type=text
	Source *imageSource `json:"source,omitempty"` // for type=image
}

type imageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g. image/jpeg, image/png
	Data      string `json:"data"`       // base64-encoded image bytes
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends a system prompt + user message and returns the model's text.
func (c *Chat) Complete(ctx context.Context, system, user string) (string, error) {
	return c.send(ctx, system, []anthropicMessage{{Role: "user", Content: user}}, 1500)
}

// CompleteVision sends a system prompt plus a user turn containing an image and
// a text instruction, and returns the model's text. [imageB64] is the raw base64
// of the image bytes; [mediaType] is e.g. "image/jpeg" or "image/png". Used to
// read a homework photo and (with a JSON-returning prompt) split it into tasks.
func (c *Chat) CompleteVision(ctx context.Context, system, user, imageB64, mediaType string) (string, error) {
	if mediaType == "" {
		mediaType = "image/jpeg"
	}
	// PDFs go in a "document" block; images in an "image" block.
	blockType := "image"
	if strings.Contains(mediaType, "pdf") {
		blockType = "document"
	}
	msg := anthropicMessage{
		Role: "user",
		Content: []contentBlock{
			{Type: blockType, Source: &imageSource{Type: "base64", MediaType: mediaType, Data: imageB64}},
			{Type: "text", Text: user},
		},
	}
	// Vision + task-splitting needs more room than a single tutor answer.
	return c.send(ctx, system, []anthropicMessage{msg}, 3000)
}

// send performs one Messages API call and concatenates the returned text blocks.
func (c *Chat) send(ctx context.Context, system string, messages []anthropicMessage, maxTokens int) (string, error) {
	cfg := c.cfg()
	if cfg.AnthropicKey == "" {
		return "", fmt.Errorf("claude: no API key configured (set it in admin Settings)")
	}
	reqBody := anthropicRequest{
		Model:     cfg.AnthropicModel,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.AnthropicKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("claude decode (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != nil {
			return "", fmt.Errorf("claude %s: %s", out.Error.Type, out.Error.Message)
		}
		return "", fmt.Errorf("claude status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var b bytes.Buffer
	for _, blk := range out.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String(), nil
}
