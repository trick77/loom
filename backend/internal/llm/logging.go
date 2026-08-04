package llm

import (
	"context"
	"log/slog"
	"time"

	"github.com/trick77/loom/internal/inference"
)

// InferenceMetadata is the per-call metadata carried on the request context. It
// is an alias, not a distinct type: the embedding and image-generation clients
// read the same context value through package inference without importing llm,
// so one metadata attach in httpapi attributes every model call the turn makes.
type InferenceMetadata = inference.Metadata

func WithInferenceMetadata(ctx context.Context, metadata InferenceMetadata) context.Context {
	return inference.WithMetadata(ctx, metadata)
}

func inferenceMetadataFromContext(ctx context.Context) InferenceMetadata {
	return inference.MetadataFromContext(ctx)
}

// observeInference records a completed model call: it emits the structured log
// line and adds this call's usage to the request's UsageAccumulator (if one is
// attached to ctx). Every successful model call funnels through here so the
// per-message token stats cover helper calls (reasoning/thread titles) and every
// tool round, not just the final answer turn.
func observeInference(ctx context.Context, model string, duration time.Duration, usage TokenUsage, finishReason string, extra ...slog.Attr) {
	logInferenceCompleted(ctx, model, duration, usage, finishReason, extra...)
	RecordUsage(ctx, usage)
}

func logInferenceCompleted(ctx context.Context, model string, duration time.Duration, usage TokenUsage, finishReason string, extra ...slog.Attr) {
	attrs := usageLogAttrs(usage)
	if finishReason != "" {
		attrs = append(attrs, slog.String("finish_reason", finishReason))
	}
	attrs = append(attrs, extra...)
	inference.LogCompleted(ctx, model, duration, attrs...)
}

func logInferenceFailed(ctx context.Context, model string, duration time.Duration, err error, extra ...slog.Attr) {
	inference.LogFailed(ctx, model, duration, err, extra...)
}

// usageLogAttrs renders the chat-completion token counts. Embeddings report a
// different usage shape and log their own counts (see rag.EmbedClient.Embed).
func usageLogAttrs(usage TokenUsage) []slog.Attr {
	if !usage.Present() {
		return nil
	}
	return []slog.Attr{
		slog.Int("prompt_tokens", usage.PromptTokens),
		slog.Int("completion_tokens", usage.CompletionTokens),
		slog.Int("total_tokens", usage.TotalTokens),
		slog.Int("cached_tokens", usage.PromptTokensDetails.CachedTokens),
		slog.Int("reasoning_tokens", usage.CompletionTokenDetails.ReasoningTokens),
	}
}
