package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/inference"
)

// The seam between loom's chat types and llmwire's.
//
// loom keeps its own Message, Tool, ToolCall, StreamEvent, StreamResult and
// TokenUsage: the httpapi layer, the tool-schema builders and the test fakes
// bind to them, and their shape is loom's business. llmwire owns the wire:
// request rendering (which output-cap spelling, the thinking toggle,
// stream_options), SSE parsing, the header / idle / call bounds, inline
// tool-call recovery, the opencode identity and per-call pricing. Everything
// that crosses the seam is converted here and nowhere else.

func toWireMessages(messages []Message) []llmwire.Message {
	out := make([]llmwire.Message, 0, len(messages))
	for _, m := range messages {
		w := llmwire.Message{
			Role:             llmwire.Role(m.Role),
			Text:             m.Content,
			ReasoningContent: m.ReasoningContent,
			ToolCallID:       m.ToolCallID,
		}
		for _, part := range m.ContentParts {
			switch {
			case part.ImageURL != nil:
				w.Parts = append(w.Parts, llmwire.Part{Kind: llmwire.PartImage, URL: part.ImageURL.URL})
			default:
				w.Parts = append(w.Parts, llmwire.Part{Kind: llmwire.PartText, Text: part.Text})
			}
		}
		for _, call := range m.ToolCalls {
			w.ToolCalls = append(w.ToolCalls, toWireToolCall(call))
		}
		out = append(out, w)
	}
	return out
}

func toWireToolCall(call ToolCall) llmwire.ToolCall {
	return llmwire.ToolCall{ID: call.ID, Type: call.Type, Name: call.Function.Name, Arguments: call.Function.Arguments}
}

func fromWireToolCall(call llmwire.ToolCall) ToolCall {
	typ := call.Type
	if typ == "" {
		typ = "function"
	}
	return ToolCall{ID: call.ID, Type: typ, Function: ToolCallFunction{Name: call.Name, Arguments: call.Arguments}}
}

func toWireTools(tools []Tool) []llmwire.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llmwire.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, llmwire.Tool{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
	}
	return out
}

// usageFromWire flattens llmwire's pointer lanes into loom's integer counts.
// Present mirrors what the endpoint reported: an object with nothing countable
// in it is "not reported", so a zero row never reads as a free call.
func usageFromWire(u llmwire.Usage) TokenUsage {
	total, ok := u.Total()
	if !ok {
		return TokenUsage{}
	}
	out := TokenUsage{
		PromptTokens:     int(llmwire.Tokens(u.Input.Total)),
		CompletionTokens: int(llmwire.Tokens(u.Output.Total)),
		TotalTokens:      int(total),
	}
	out.PromptTokensDetails.CachedTokens = int(llmwire.Tokens(u.Input.CacheRead))
	out.CompletionTokenDetails.ReasoningTokens = int(llmwire.Tokens(u.Output.Reasoning))
	return out
}

// costFromWire reads the priced figure. Unpriced is not zero: it is unknown,
// and the caller keeps it out of every sum.
func costFromWire(u llmwire.Usage) (nanoUSD int64, priced bool) {
	if u.Cost.Provenance == llmwire.Unpriced {
		return 0, false
	}
	return u.Cost.NanoUSD, true
}

// chatError phrases a wire failure for logs and the user-facing message. A
// status error reads "chat completion failed with status N"; every error stays
// wrapped, so llmwire's classes (ErrRateLimited, ErrAuth, ...) and the concrete
// *APIError remain reachable with errors.Is/As.
func chatError(err error) error {
	if err == nil {
		return nil
	}
	// A stall before the first byte (an upstream queueing past the window) and
	// one mid-stream are the same failure to the handlers: the model stopped
	// responding.
	if errors.Is(err, llmwire.ErrStreamIdle) || errors.Is(err, llmwire.ErrNoResponseHeaders) {
		return fmt.Errorf("read chat completion stream: %w", ErrStreamStalled)
	}
	var apiErr *llmwire.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return inference.WireError(fmt.Sprintf("chat completion failed with status %d: %s", apiErr.StatusCode, apiErr.Message), err)
	}
	return fmt.Errorf("chat completion request: %w", err)
}

// logWarnings records llmwire's warnings. Most are debug noise (the common one
// is "no usage object" on a helper call). Three are operator problems and log
// at WARN:
//   - tool_calls: inline tool-call markup the model leaked into its text,
//     recovered into a call or cut without one; the client never saw it.
//   - max_tokens: a cap past the model's output limit, which the endpoint
//     clamps silently.
//   - cost: a call llmwire could not price, a hole in every cost figure. Once
//     per process and model: every call would repeat it, and the fix is in
//     llmwire's profile, not in loom.
func logWarnings(ctx context.Context, model string, warnings []llmwire.Warning) {
	for _, w := range warnings {
		level := slog.LevelDebug
		switch w.Feature {
		case "tool_calls", "max_tokens":
			level = slog.LevelWarn
		case "cost":
			if _, seen := warnedUnpriced.LoadOrStore(model, struct{}{}); !seen {
				level = slog.LevelWarn
			}
		}
		slog.LogAttrs(ctx, level, "llm: wire warning",
			slog.String("model", model),
			slog.String("kind", string(w.Kind)),
			slog.String("feature", w.Feature),
			slog.String("details", w.Details))
	}
}

// warnedUnpriced holds the models whose cost warning already went out at WARN.
var warnedUnpriced sync.Map

// streamProgressAttrs reports per-stream observability so a stall is diagnosable
// after the fact and the idle window is calibratable from healthy turns: time to
// the first data frame, total SSE bytes, and the worst silent gap between data
// frames seeded at request start — exactly the window the idle watchdog races
// against, so compare it to the configured idle timeout to judge the margin.
func streamProgressAttrs(res llmwire.StreamResult) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int64("stream_bytes", res.Bytes),
		slog.Int64("max_idle_ms", res.MaxDataGap.Milliseconds()),
		slog.Int64("headers_ms", res.Timing.Headers.Milliseconds()),
	}
	if res.Timing.FirstData > 0 {
		attrs = append(attrs, slog.Int64("first_token_ms", res.Timing.FirstData.Milliseconds()))
	}
	return attrs
}

// durationOr returns the measured duration, or the wall-clock fallback when
// the wire did not measure (a failure before the request went out).
func durationOr(measured time.Duration, start time.Time) time.Duration {
	if measured > 0 {
		return measured
	}
	return time.Since(start)
}
