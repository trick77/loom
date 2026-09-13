package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/trick77/llmwire"
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
	if _, ok := u.Total(); !ok {
		return TokenUsage{}
	}
	out := TokenUsage{
		PromptTokens:     int(llmwire.Tokens(u.Input.Total)),
		CompletionTokens: int(llmwire.Tokens(u.Output.Total)),
	}
	out.TotalTokens = out.PromptTokens + out.CompletionTokens
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

// chatError phrases a wire failure the way loom's handlers already read it. A
// status error keeps the "chat completion failed with status N" wording the
// httpapi layer matches on; llmwire's own bounds and shape errors pass through
// with their names, wrapped so errors.Is still holds.
func chatError(err error) error {
	if err == nil {
		return nil
	}
	// A stall before the first byte (MiMo Pro queueing past the window) and one
	// mid-stream are the same failure to the handlers: the model stopped
	// responding.
	if errors.Is(err, llmwire.ErrStreamIdle) || errors.Is(err, llmwire.ErrNoResponseHeaders) {
		return fmt.Errorf("read chat completion stream: %w", ErrStreamStalled)
	}
	var apiErr *llmwire.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return fmt.Errorf("chat completion failed with status %d: %s", apiErr.StatusCode, apiErr.Message)
	}
	return fmt.Errorf("chat completion request: %w", err)
}

// logWarnings records llmwire's warnings. Most are debug noise (the common one
// is "no usage object" on a helper call). The tool_calls feature is the
// exception: it reports inline tool-call markup that the model leaked into its
// text, recovered into a call or cut without one, which the operator needs to
// see at the default log level because the client never saw that text.
func logWarnings(ctx context.Context, model string, warnings []llmwire.Warning) {
	for _, w := range warnings {
		level := slog.LevelDebug
		if w.Feature == "tool_calls" {
			level = slog.LevelWarn
		}
		slog.LogAttrs(ctx, level, "llm: wire warning",
			slog.String("model", model),
			slog.String("kind", string(w.Kind)),
			slog.String("feature", w.Feature),
			slog.String("details", w.Details))
	}
}

// warnUnpricedOnce keeps the rate-table warning to one line per process and
// model: every call would otherwise repeat it, and the fix is in llmwire's
// profile, not in loom.
var warnUnpricedOnce sync.Map

func noteUnpriced(ctx context.Context, model string) {
	if _, seen := warnUnpricedOnce.LoadOrStore(model, struct{}{}); !seen {
		slog.WarnContext(ctx, "llm: no rate for model, cost not accounted", slog.String("model", model))
	}
}

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
