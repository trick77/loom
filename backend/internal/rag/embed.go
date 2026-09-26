package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/inference"
)

const defaultEmbedTimeout = 1 * time.Minute

// EmbedModel is the configured embedding model as its llmwire profile states
// it: the vector width the vec_chunks table must match, and the key variable
// its provider reads.
type EmbedModel struct {
	ID     string
	Width  int
	KeyEnv string
}

// ResolveEmbedModel looks id up as an embedding model. An unknown id, or one
// that is not an embedding model, is an error naming the valid choices.
func ResolveEmbedModel(reg *llmwire.Registry, id string) (EmbedModel, error) {
	if reg == nil {
		reg = llmwire.Default()
	}
	p, err := reg.LookupEmbedding(id)
	if err != nil {
		return EmbedModel{}, fmt.Errorf("rag: embedding model: %w; valid choices are %s",
			err, strings.Join(embeddingModels(reg), ", "))
	}
	return EmbedModel{ID: p.ID, Width: p.Embedding.DefaultDimensions, KeyEnv: p.APIKeyEnv()}, nil
}

// EmbedKeyEnvs lists the key variables of every embedding model's provider in
// the registry, each once: a set one without a configured embedding model is
// how an upgrade from the key-only configuration shows.
func EmbedKeyEnvs(reg *llmwire.Registry) []string {
	if reg == nil {
		reg = llmwire.Default()
	}
	var out []string
	for _, id := range embeddingModels(reg) {
		p, err := reg.LookupEmbedding(id)
		if err != nil {
			continue
		}
		if env := p.APIKeyEnv(); env != "" && !slices.Contains(out, env) {
			out = append(out, env)
		}
	}
	return out
}

func embeddingModels(reg *llmwire.Registry) []string {
	var out []string
	for _, id := range reg.Models() {
		if _, err := reg.LookupEmbedding(id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// EmbedClient generates embeddings through llmwire.
type EmbedClient struct {
	wire  *llmwire.Client
	model string
}

// EmbedConfig holds the embedding client settings loom owns. Model is the
// llmwire registry id; Registry is nil for llmwire's default (tests pass a
// synthetic one). BaseURL is an explicit override for a test fake and bypasses
// the environment.
type EmbedConfig struct {
	Model    string
	Registry *llmwire.Registry
	BaseURL  string
	APIKey   string
}

// NewEmbedClient builds an EmbedClient. httpClient is optional. The error is
// an unusable model or a missing key variable, named.
func NewEmbedClient(cfg EmbedConfig, httpClient *http.Client) (*EmbedClient, error) {
	if _, err := ResolveEmbedModel(cfg.Registry, cfg.Model); err != nil {
		return nil, err
	}
	wire, err := llmwire.FromEnv(cfg.Model, llmwire.Config{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		HTTPClient:  httpClient,
		CallTimeout: defaultEmbedTimeout,
		Registry:    cfg.Registry,
	})
	if err != nil {
		return nil, err
	}
	return &EmbedClient{wire: wire, model: cfg.Model}, nil
}

// EmbeddingUsage is one call's token accounting. Present says the endpoint
// reported it; CostPriced says llmwire had a rate for the model, and an
// unpriced call stays out of every sum rather than counting as free.
type EmbeddingUsage struct {
	PromptTokens int  `json:"prompt_tokens"`
	TotalTokens  int  `json:"total_tokens"`
	Present      bool `json:"-"`
	CostNanoUSD  int64
	CostPriced   bool
}

// EmbedResult holds the vectors and usage metrics from an embedding operation.
type EmbedResult struct {
	Vectors [][]float32
	Usage   EmbeddingUsage
}

func usageFromWire(u llmwire.Usage) EmbeddingUsage {
	total, ok := u.Total()
	if !ok {
		return EmbeddingUsage{}
	}
	out := EmbeddingUsage{
		PromptTokens: int(llmwire.Tokens(u.Input.Total)),
		TotalTokens:  int(total),
		Present:      true,
	}
	if u.Cost.Provenance != llmwire.Unpriced {
		out.CostNanoUSD, out.CostPriced = u.Cost.NanoUSD, true
	}
	return out
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

	resp, warnings, err := c.wire.Embed(ctx, llmwire.EmbedRequest{Model: c.model, Inputs: inputs})
	for _, w := range warnings {
		slog.DebugContext(ctx, "embed: wire warning", slog.String("model", c.model), slog.String("warning", w.String()))
	}
	if err != nil {
		err = embedError(err)
		inference.LogFailed(ctx, c.model, time.Since(start), err, inputCount)
		return EmbedResult{}, err
	}
	usage := usageFromWire(resp.Usage)
	attrs := []slog.Attr{inputCount}
	if usage.Present {
		attrs = append(attrs,
			slog.Int("prompt_tokens", usage.PromptTokens),
			slog.Int("total_tokens", usage.TotalTokens),
		)
	}
	if usage.CostPriced {
		attrs = append(attrs, slog.Int64("cost_nano_usd", usage.CostNanoUSD))
	}
	duration := resp.Timing.Total
	if duration == 0 {
		duration = time.Since(start)
	}
	inference.LogCompleted(ctx, c.model, duration, attrs...)
	return EmbedResult{Vectors: resp.Vectors, Usage: usage}, nil
}

// embedError phrases a wire failure the way the rest of loom reads it: a
// status error reads "embedding failed with status N"; every error stays
// wrapped so llmwire's classes remain reachable.
func embedError(err error) error {
	var apiErr *llmwire.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return inference.WireError(fmt.Sprintf("embedding failed with status %d: %s", apiErr.StatusCode, apiErr.Message), err)
	}
	return fmt.Errorf("embed request: %w", err)
}
