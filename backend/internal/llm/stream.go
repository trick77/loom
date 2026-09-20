package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/trick77/llmwire"
)

// ErrStreamStalled marks a stream that was aborted by the idle bound because
// the model stopped emitting chunks for longer than the configured idle window. It
// is deliberately distinct from context.Canceled so callers can tell a stalled
// upstream apart from a client disconnect, and surface a clear message.
var ErrStreamStalled = errors.New("the model stopped responding")

func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	result, err := c.StreamChatResult(ctx, messages, onDelta)
	return result.Content, err
}

func (c *Client) StreamChatResult(ctx context.Context, messages []Message, onDelta func(string) error) (StreamResult, error) {
	result, err := c.StreamChatWithTools(ctx, messages, nil, func(event StreamEvent) error {
		if event.Delta == "" || onDelta == nil {
			return nil
		}
		return onDelta(event.Delta)
	})
	return result, err
}

// StreamChatWithTools runs one streamed model turn. llmwire owns the wire: the
// request body, the SSE parse, the header / idle / call bounds, and the
// recovery of tool calls MiMo writes as inline markup (its profile flag), which
// arrive here as ordinary tool-call events with the markup already withheld
// from the content and reasoning deltas. What loom adds is the routing (model,
// effort, budgets per tool set), the event shape its handlers consume, and the
// accounting.
func (c *Client) StreamChatWithTools(ctx context.Context, messages []Message, tools []Tool, onEvent func(StreamEvent) error) (StreamResult, error) {
	start := time.Now()
	// Single routing decision for the whole turn: vision model iff the payload
	// carries an image part, else the text model. The same `messages` slice is
	// re-sent on every tool round within this turn, so the choice stays stable.
	model := c.modelForMessages(messages)
	// One reasoning-effort decision for the whole turn (matching the single model
	// decision above): the composer's per-request choice from the context, else the
	// configured default. Reused for both the outbound request and StreamResult.
	reasoningEffort := c.resolveReasoningEffort(ctx)
	// Per-turn overrides carried on the context (set by the httpapi layer): the
	// forced-final answer turn disables thinking and widens the completion budget.
	meta := inferenceMetadataFromContext(ctx)
	maxCompletionTokens := c.maxCompletionTokensForTools(tools)
	if meta.MaxCompletionTokens > 0 {
		maxCompletionTokens = meta.MaxCompletionTokens
	}
	req := llmwire.ChatRequest{
		Model:     model,
		Messages:  toWireMessages(messages),
		Tools:     toWireTools(tools),
		MaxTokens: &maxCompletionTokens,
		// Once a tool call is underway on a document-capable turn the idle
		// bound widens to the document timeout; every other turn keeps the
		// narrow window. See toolCallIdleTimeout.
		ToolCallIdleTimeout: toolCallIdleTimeout(tools),
	}
	if meta.SuppressThinking {
		// The forced final answer: thinking off on the wire (MiMo's toggle),
		// while reasoningEffort still feeds StreamResult.ReasoningEffort so the
		// persisted message keeps the composer's chosen effort, which governed
		// the reasoning across the tool rounds, rather than recording a blank.
		req.Reasoning = llmwire.ReasoningOff()
	} else {
		req.Reasoning = llmwire.ReasoningEffort(reasoningEffort)
	}
	callCtx := ctx
	if timeout := c.timeoutForTools(tools); timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	stream, warnings, err := c.wire.ChatStream(callCtx, req)
	logWarnings(ctx, model, warnings)
	if err != nil {
		err = chatError(err)
		logInferenceFailed(ctx, model, time.Since(start), err)
		return StreamResult{}, err
	}
	defer func() { _ = stream.Close() }()

	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int
	// Emitted at most once per turn, the moment we first know a tool call is
	// underway — well before the parsed call surfaces at the end of the stream.
	// Lets the client keep showing "thinking" during MiMo's silent
	// argument-serialization gap instead of settling on a reasoning summary.
	toolPendingEmitted := false
	// The first fragment of a call carries its id and name, then the (large)
	// argument streams or bursts over later chunks. Surface the name once per
	// call as soon as it is known so the client can show the running tool
	// immediately; the full call re-emitted at end-of-stream carries the same id
	// and updates the same entry instead of duplicating it. Inline-recovered
	// calls arrive the same way (name first, arguments last).
	nameEmitted := map[int]bool{}
	var finishReason string
	emit := func(ev StreamEvent) error {
		if onEvent == nil {
			return nil
		}
		return onEvent(ev)
	}
	var consumerErr error
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case llmwire.EventContent:
			consumerErr = emit(StreamEvent{Delta: ev.Text})
		case llmwire.EventReasoning:
			consumerErr = emit(StreamEvent{ReasoningDelta: ev.Text})
		case llmwire.EventToolCall:
			if !toolPendingEmitted {
				toolPendingEmitted = true
				if consumerErr = emit(StreamEvent{ToolPending: true}); consumerErr != nil {
					break
				}
			}
			call, ok := toolCalls[ev.Index]
			if !ok {
				call = &ToolCall{Type: "function"}
				toolCalls[ev.Index] = call
				toolCallOrder = append(toolCallOrder, ev.Index)
			}
			if ev.ID != "" {
				call.ID = ev.ID
			}
			if ev.Name != "" {
				call.Function.Name = ev.Name
			}
			call.Function.Arguments += ev.ArgumentsDelta
			if call.Function.Name != "" && !nameEmitted[ev.Index] {
				nameEmitted[ev.Index] = true
				consumerErr = emit(StreamEvent{ToolCall: ToolCall{ID: call.ID, Type: call.Type, Function: ToolCallFunction{Name: call.Function.Name}}})
			}
		case llmwire.EventFinish:
			finishReason = ev.FinishReason
		}
		if consumerErr != nil {
			break
		}
	}
	// Close waits for the reader to finish, so Result() holds everything that
	// streamed before a consumer error too: the failure log and the title fall
	// back on that partial answer. On a clean drain Close is a no-op and Err
	// carries the wire's own failure.
	closeErr := stream.Close()
	res := stream.Result()
	if consumerErr == nil {
		consumerErr = closeErr
	}
	logWarnings(ctx, model, stream.Warnings())
	partial := StreamResult{
		Content:          res.Content,
		ReasoningContent: res.Reasoning,
		Usage:            usageFromWire(res.Usage),
	}
	// progress carries the RCA-decisive shape of the turn so a failure mode is
	// readable straight off the log line: which channel was being emitted and how
	// much. content_bytes vs reasoning_bytes vs tool_arg_bytes distinguishes a
	// content stall, a reasoning/serialization spiral, and a tool-argument stall
	// (large tool_arg_bytes that then goes silent). Only byte counts and the tool
	// name — never payload contents — keep entries short.
	progress := func() []slog.Attr {
		attrs := streamProgressAttrs(res)
		attrs = append(attrs,
			slog.Int("content_bytes", len(res.Content)),
			slog.Int("reasoning_bytes", len(res.Reasoning)),
		)
		if n := toolArgBytes(toolCalls); n > 0 {
			attrs = append(attrs, slog.Int("tool_arg_bytes", n))
		}
		if name := firstToolName(toolCalls, toolCallOrder); name != "" {
			attrs = append(attrs, slog.String("tool", name))
		}
		return attrs
	}
	if consumerErr != nil {
		var err error
		if closeErr == nil {
			// The wire was fine; the consumer (the SSE write to the browser)
			// failed, and the error must not read as an upstream failure.
			err = fmt.Errorf("deliver chat completion stream: %w", consumerErr)
		} else {
			err = chatError(consumerErr)
		}
		logInferenceFailed(ctx, model, durationOr(res.Timing.Total, start), err, progress()...)
		return partial, err
	}

	result := partial
	result.FinishReason = finishReason
	result.ToolCalls = make([]ToolCall, 0, len(res.ToolCalls))
	for _, call := range res.ToolCalls {
		converted := fromWireToolCall(call)
		result.ToolCalls = append(result.ToolCalls, converted)
		if err := emit(StreamEvent{ToolCall: converted}); err != nil {
			return result, err
		}
	}
	result.Duration = durationOr(res.Timing.Total, start)
	result.Model = model
	result.ReasoningEffort = reasoningEffort
	result.CostNanoUSD, result.CostPriced = costFromWire(res.Usage)
	if !result.CostPriced {
		noteUnpriced(ctx, model)
	}
	observeInference(ctx, model, result.Duration, result.Usage, result.FinishReason, progress()...)
	RecordCost(ctx, result.CostNanoUSD, result.CostPriced)
	return result, nil
}

// toolArgBytes sums the streamed tool-call argument lengths accumulated so far —
// the size of what the model is serializing into tool calls, which on a stalled
// document turn is exactly the payload that never finished.
func toolArgBytes(byIndex map[int]*ToolCall) int {
	total := 0
	for _, call := range byIndex {
		total += len(call.Function.Arguments)
	}
	return total
}

// firstToolName returns the name of the first tool call seen, so a turn that is
// (or was) emitting a tool argument is identifiable even when it never completed.
func firstToolName(byIndex map[int]*ToolCall, order []int) string {
	for _, index := range order {
		if call, ok := byIndex[index]; ok && call.Function.Name != "" {
			return call.Function.Name
		}
	}
	return ""
}
