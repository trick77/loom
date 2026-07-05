package llm

import (
	"context"
	"log/slog"
	"time"
)

type inferenceMetadataKey struct{}

type InferenceMetadata struct {
	UserID   string
	Username string
	ThreadID string
	Purpose  string
	Round    int
	// ReasoningEffort is the per-request reasoning depth chosen for this turn
	// (low/medium/high). Empty falls back to the client's default. It rides the
	// context so the httpapi layer can steer the effort without threading a new
	// argument through every StreamChatWithTools call site; utility/title calls
	// leave it empty and keep the default. See resolveReasoningEffort.
	ReasoningEffort string
	// SuppressThinking turns MiMo's native thinking off for this turn
	// ({"thinking":{"type":"disabled"}}), the same lever the utility calls use.
	// Set on the forced-final answer turns: by then all research reasoning has
	// already happened across the tool rounds and sits in history, so the model
	// only needs to write the answer — leaving thinking on lets it burn the whole
	// completion budget on reasoning and emit no prose (finish_reason=length).
	SuppressThinking bool
	// MaxCompletionTokens overrides the turn's completion-token cap when > 0. The
	// forced-final answer sets a larger budget than the default chat cap so a
	// synthesis over many gathered sources has room to complete.
	MaxCompletionTokens int
	// Incognito marks an ephemeral turn whose content must never be written to
	// disk. It suppresses the dev-only response-body log (see wrapResponseLogger)
	// so an incognito transcript leaves no trace even when response logging is on.
	Incognito bool
}

func WithInferenceMetadata(ctx context.Context, metadata InferenceMetadata) context.Context {
	return context.WithValue(ctx, inferenceMetadataKey{}, metadata)
}

func inferenceMetadataFromContext(ctx context.Context) InferenceMetadata {
	metadata, _ := ctx.Value(inferenceMetadataKey{}).(InferenceMetadata)
	return metadata
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
	attrs := inferenceLogAttrs(ctx, model, duration, usage)
	if finishReason != "" {
		attrs = append(attrs, slog.String("finish_reason", finishReason))
	}
	attrs = append(attrs, extra...)
	slog.LogAttrs(ctx, slog.LevelInfo, "llm inference completed", attrs...)
}

func logInferenceFailed(ctx context.Context, model string, duration time.Duration, err error, extra ...slog.Attr) {
	attrs := inferenceLogAttrs(ctx, model, duration, TokenUsage{})
	attrs = append(attrs, slog.String("err", err.Error()))
	if cause := context.Cause(ctx); cause != nil {
		attrs = append(attrs, slog.String("cancel_cause", cause.Error()))
	}
	attrs = append(attrs, extra...)
	slog.LogAttrs(ctx, slog.LevelError, "llm inference failed", attrs...)
}

func inferenceLogAttrs(ctx context.Context, model string, duration time.Duration, usage TokenUsage) []slog.Attr {
	metadata := inferenceMetadataFromContext(ctx)
	attrs := []slog.Attr{
		slog.String("model", model),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}
	if metadata.UserID != "" {
		attrs = append(attrs, slog.String("user_id", metadata.UserID))
	}
	if metadata.Username != "" {
		attrs = append(attrs, slog.String("username", metadata.Username))
	}
	if metadata.ThreadID != "" {
		attrs = append(attrs, slog.String("thread_id", metadata.ThreadID))
	}
	if metadata.Purpose != "" {
		attrs = append(attrs, slog.String("purpose", metadata.Purpose))
	}
	if metadata.Round != 0 {
		attrs = append(attrs, slog.Int("round", metadata.Round))
	}
	if usage.Present() {
		attrs = append(attrs,
			slog.Int("prompt_tokens", usage.PromptTokens),
			slog.Int("completion_tokens", usage.CompletionTokens),
			slog.Int("total_tokens", usage.TotalTokens),
			slog.Int("cached_tokens", usage.PromptTokensDetails.CachedTokens),
			slog.Int("reasoning_tokens", usage.CompletionTokenDetails.ReasoningTokens),
		)
	}
	return attrs
}
