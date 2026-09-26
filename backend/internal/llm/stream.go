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

// StreamChat runs a streamed model turn and returns just the content string, calling onDelta for each text chunk.
func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error) {
	result, err := c.StreamChatResult(ctx, messages, onDelta)
	return result.Content, err
}

// StreamChatResult runs a streamed model turn and returns the full result including usage and metadata.
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
// request body, the SSE parse, the header / idle / call bounds, and, for a
// model whose profile flags it, the recovery of tool calls written as inline
// markup, which arrive here as ordinary tool-call events with the markup
// already withheld from the content and reasoning deltas. What loom adds is the routing (model,
// effort, budgets per tool set), the event shape its handlers consume, and the
// accounting.
func (c *Client) StreamChatWithTools(ctx context.Context, messages []Message, tools []Tool, onEvent func(StreamEvent) error) (StreamResult, error) {
	start := time.Now()
	// Per-turn overrides carried on the context (set by the httpapi layer): the
	// forced-final answer turn disables thinking and widens the completion budget.
	meta := inferenceMetadataFromContext(ctx)
	// Single routing decision for the whole turn: the prose model when thinking
	// is off, else the vision model iff the payload carries an image part, else
	// the text model. The same `messages` slice is re-sent on every tool round
	// within this turn, so the choice stays stable.
	model := c.modelForMessages(messages, meta.SuppressThinking)
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
	// Every turn names its effort: unset means the vendor's max (see the
	// reasoning note in client.go). The forced final answer takes the helper
	// level, because deep thinking there burns the whole completion budget
	// reasoning and emits no prose.
	effort := turnReasoningEffort
	if meta.SuppressThinking {
		effort = helperReasoningEffort
	}
	req.Reasoning = llmwire.ReasoningEffort(effort)
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
	// Lets the client keep showing "thinking" during a silent
	// argument-serialization gap instead of settling on a reasoning summary.
	toolPendingEmitted := false
	// The first fragment of a call carries its id and name, then the (large)
	// argument streams or bursts over later chunks. Surface the name once per
	// call as soon as it is known so the client can show the running tool
	// immediately; the full call re-emitted at end-of-stream carries the same id
	// and updates the same entry instead of duplicating it. That only holds when
	// the early event already carries the id, so when the name lands a chunk
	// before the id the emit waits for the id. A call that never gets one (an
	// inline-recovered call arrives name first, arguments last, no id) is
	// surfaced once its arguments start, since an id would have preceded them.
	nameEmitted := map[int]bool{}
	// argBytes counts streamed argument bytes per call for the progress log; the
	// text itself is not accumulated here, the wire's Result carries it.
	argBytes := map[int]int{}
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
			argBytes[ev.Index] += len(ev.ArgumentsDelta)
			idKnown := call.ID != "" || ev.ArgumentsDelta != ""
			if call.Function.Name != "" && idKnown && !nameEmitted[ev.Index] {
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
		if n := toolArgBytes(argBytes); n > 0 {
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
	result.ReasoningEffort = effort
	result.CostNanoUSD, result.CostPriced = costFromWire(res.Usage)
	observeInference(ctx, model, result.Duration, result.Usage, result.FinishReason, progress()...)
	RecordCost(ctx, result.CostNanoUSD, result.CostPriced)
	return result, nil
}

// toolArgBytes sums the streamed tool-call argument bytes counted so far — the
// size of what the model is serializing into tool calls, which on a stalled
// document turn is exactly the payload that never finished.
func toolArgBytes(byIndex map[int]int) int {
	total := 0
	for _, n := range byIndex {
		total += n
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
