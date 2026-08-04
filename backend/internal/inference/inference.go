// Package inference holds the per-call metadata that rides the request context
// and the structured log lines every model call emits.
//
// It exists so the non-chat model clients — rag.EmbedClient (embeddings) and
// imagegen.BFLClient (image generation) — can log with the same message and the
// same attribution attrs as the chat client without importing package llm
// (which they deliberately do not depend on). One ctx key, one log shape:
// `msg="llm inference completed"` / `msg="llm inference failed"` covers every
// model call in the app, so a single grep accounts for all of them.
//
// Prompts and inputs are never logged here, by design — only the metadata,
// timing, token counts and per-call shape attrs the caller passes in.
package inference

import (
	"context"
	"log/slog"
	"time"
)

type metadataKey struct{}

// Metadata describes the request a model call belongs to. Anything set here
// shows up on the call's log line; the llm-specific fields additionally steer
// how the chat request is built.
type Metadata struct {
	UserID   string
	Username string
	ThreadID string
	Purpose  string
	Round    int
	// ReasoningEffort is the per-request reasoning depth chosen for this turn
	// (low/medium/high). Empty falls back to the client's default. It rides the
	// context so the httpapi layer can steer the effort without threading a new
	// argument through every StreamChatWithTools call site; utility/title calls
	// leave it empty and keep the default. See llm.resolveReasoningEffort.
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
	// disk. It suppresses the dev-only response-body log (see
	// llm.wrapResponseLogger) so an incognito transcript leaves no trace even when
	// response logging is on.
	Incognito bool
}

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, metadataKey{}, metadata)
}

func MetadataFromContext(ctx context.Context) Metadata {
	metadata, _ := ctx.Value(metadataKey{}).(Metadata)
	return metadata
}

// WithPurpose re-tags the context's metadata for a specific call, keeping the
// user/thread attribution already on it. Used where one request makes model
// calls of several kinds (e.g. a chat turn that also embeds a RAG query).
func WithPurpose(ctx context.Context, purpose string) context.Context {
	metadata := MetadataFromContext(ctx)
	metadata.Purpose = purpose
	return WithMetadata(ctx, metadata)
}

// WithDefaultPurpose tags the call only when the caller has not already chosen a
// more specific purpose. Model clients use it so their log line always carries a
// purpose, even from a call path that attached no metadata at all.
func WithDefaultPurpose(ctx context.Context, purpose string) context.Context {
	if MetadataFromContext(ctx).Purpose != "" {
		return ctx
	}
	return WithPurpose(ctx, purpose)
}

// LogCompleted emits the success line for one model call. extra carries the
// per-client attrs (token usage, finish reason, stream progress, image size…).
func LogCompleted(ctx context.Context, model string, duration time.Duration, extra ...slog.Attr) {
	attrs := append(BaseAttrs(ctx, model, duration), extra...)
	slog.LogAttrs(ctx, slog.LevelInfo, "llm inference completed", attrs...)
}

// LogFailed emits the failure line for one model call. Every model client logs
// through here on every error path, so a call that never completes still leaves
// exactly one accounted-for line.
func LogFailed(ctx context.Context, model string, duration time.Duration, err error, extra ...slog.Attr) {
	attrs := BaseAttrs(ctx, model, duration)
	attrs = append(attrs, slog.String("err", err.Error()))
	if cause := context.Cause(ctx); cause != nil {
		attrs = append(attrs, slog.String("cancel_cause", cause.Error()))
	}
	attrs = append(attrs, extra...)
	slog.LogAttrs(ctx, slog.LevelError, "llm inference failed", attrs...)
}

// BaseAttrs builds the attrs shared by every model call: what was called, how
// long it took, and who it was for.
func BaseAttrs(ctx context.Context, model string, duration time.Duration) []slog.Attr {
	metadata := MetadataFromContext(ctx)
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
	return attrs
}
