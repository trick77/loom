package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/inference"
)

// EmbedModel is the embedding model, a constant of the build: the vector
// column width is fixed by the migration that created it (see EmbedDim), so
// swapping the model is a re-index, not a config change. The profile ships the
// host; the key comes from LLMWIRE_OPENAI_API_KEY, the variable the profile's
// provider names.
const EmbedModel = "text-embedding-3-small"

const defaultEmbedTimeout = 1 * time.Minute

// embedProfile is the model's llmwire profile, resolved once so a typo in the
// constant or a model that is not an embeddings model fails at init.
var embedProfile = mustEmbedProfile()

func mustEmbedProfile() *llmwire.Profile {
	p, err := llmwire.Default().LookupEmbedding(EmbedModel)
	if err != nil {
		panic(err)
	}
	return p
}

// EmbedDim is the vector width the model returns, from its profile. The
// sqlite-vec column must match it; a test pins the migration DDL to this.
func EmbedDim() int {
	return embedProfile.Embedding.DefaultDimensions
}

// EmbedAPIKeyEnv is the variable the embeddings key is read from
// (llmwire.FromEnv): the profile's provider decides the name. A set value
// turns embeddings on.
func EmbedAPIKeyEnv() string {
	return embedProfile.APIKeyEnv()
}

// EmbedClient generates embeddings through llmwire.
type EmbedClient struct {
	wire *llmwire.Client
}

// EmbedConfig holds the embedding client settings loom owns. BaseURL is an
// explicit override for a test fake and bypasses the environment.
type EmbedConfig struct {
	BaseURL string
	APIKey  string
}

// NewEmbedClient builds an EmbedClient. httpClient is optional. The error is a
// missing LLMWIRE_OPENAI_API_KEY, named.
func NewEmbedClient(cfg EmbedConfig, httpClient *http.Client) (*EmbedClient, error) {
	wire, err := llmwire.FromEnv(EmbedModel, llmwire.Config{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		HTTPClient:  httpClient,
		CallTimeout: defaultEmbedTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &EmbedClient{wire: wire}, nil
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

	resp, warnings, err := c.wire.Embed(ctx, llmwire.EmbedRequest{Model: EmbedModel, Inputs: inputs})
	for _, w := range warnings {
		slog.DebugContext(ctx, "embed: wire warning", slog.String("model", EmbedModel), slog.String("warning", w.String()))
	}
	if err != nil {
		err = embedError(err)
		inference.LogFailed(ctx, EmbedModel, time.Since(start), err, inputCount)
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
	inference.LogCompleted(ctx, EmbedModel, duration, attrs...)
	return EmbedResult{Vectors: resp.Vectors, Usage: usage}, nil
}

// embedError phrases a wire failure the way the rest of loom reads it: a
// status error keeps the "embedding failed with status N" wording the ingest
// path and its tests match on; everything else keeps llmwire's own naming.
func embedError(err error) error {
	var apiErr *llmwire.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return fmt.Errorf("embedding failed with status %d: %s", apiErr.StatusCode, apiErr.Message)
	}
	return fmt.Errorf("embed request: %w", err)
}
