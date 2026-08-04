package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/trick77/loom/internal/inference"
)

const (
	defaultEmbedTimeout = 1 * time.Minute
	maxEmbedErrorBody   = 4 << 10
)

// EmbedConfig configures the OpenAI-compatible embedding client. It reuses the
// app's BACKEND_EMBED_* settings (separate from the MiMo chat endpoint).
type EmbedConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// EmbedClient generates embeddings via an OpenAI-compatible /embeddings endpoint.
type EmbedClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewEmbedClient builds an EmbedClient. httpClient is optional.
func NewEmbedClient(cfg EmbedConfig, httpClient *http.Client) *EmbedClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultEmbedTimeout}
	}
	return &EmbedClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		httpClient: httpClient,
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage json.RawMessage `json:"usage"`
}

type EmbeddingUsage struct {
	PromptTokens int  `json:"prompt_tokens"`
	TotalTokens  int  `json:"total_tokens"`
	Present      bool `json:"-"`
}

func (u *EmbeddingUsage) UnmarshalJSON(data []byte) error {
	var raw struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.PromptTokens = raw.PromptTokens
	u.TotalTokens = raw.TotalTokens
	u.Present = true
	return nil
}

type EmbedResult struct {
	Vectors [][]float32
	Usage   EmbeddingUsage
}

func parseEmbeddingUsage(raw json.RawMessage) EmbeddingUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return EmbeddingUsage{}
	}
	var usage EmbeddingUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return EmbeddingUsage{}
	}
	if usage.PromptTokens == 0 && usage.TotalTokens == 0 {
		return EmbeddingUsage{}
	}
	usage.Present = true
	return usage
}

// Embed returns one embedding vector per input, aligned to the input order.
// An empty input yields no vectors without making a request.
//
// Every request emits one inference log line — success or failure — so the
// embedding model is accounted for alongside the chat and image calls. The
// inputs themselves are never logged, only how many there were.
func (c *EmbedClient) Embed(ctx context.Context, inputs []string) (EmbedResult, error) {
	if len(inputs) == 0 {
		return EmbedResult{}, nil
	}
	ctx = inference.WithDefaultPurpose(ctx, "embed")
	start := time.Now()
	inputCount := slog.Int("input_count", len(inputs))
	fail := func(err error) (EmbedResult, error) {
		inference.LogFailed(ctx, c.model, time.Since(start), err, inputCount)
		return EmbedResult{}, err
	}

	body, err := json.Marshal(embedRequest{Model: c.model, Input: inputs})
	if err != nil {
		return fail(fmt.Errorf("marshal embed request: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return fail(fmt.Errorf("create embed request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fail(fmt.Errorf("embed request: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxEmbedErrorBody))
		return fail(fmt.Errorf("embedding failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg))))
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fail(fmt.Errorf("decode embed response: %w", err))
	}
	if len(parsed.Data) != len(inputs) {
		return fail(fmt.Errorf("embedding count mismatch: got %d, want %d", len(parsed.Data), len(inputs)))
	}

	// The spec allows out-of-order data; sort by index to realign to inputs.
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	usage := parseEmbeddingUsage(parsed.Usage)
	attrs := []slog.Attr{inputCount}
	if usage.Present {
		attrs = append(attrs,
			slog.Int("prompt_tokens", usage.PromptTokens),
			slog.Int("total_tokens", usage.TotalTokens),
		)
	}
	inference.LogCompleted(ctx, c.model, time.Since(start), attrs...)
	return EmbedResult{Vectors: out, Usage: usage}, nil
}
