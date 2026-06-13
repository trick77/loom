package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ErrStreamStalled marks a stream that was aborted by the idle watchdog because
// the model stopped emitting chunks for longer than the configured idle window. It
// is deliberately distinct from context.Canceled so callers can tell a stalled
// upstream apart from a client disconnect, and surface a clear message.
var ErrStreamStalled = errors.New("the model stopped responding")

// streamProgressAttrs reports per-stream observability so a stall is diagnosable
// after the fact and the idle window is calibratable from healthy turns: time to
// the first token, total SSE bytes, time since the last delta when the stream
// ended, and the worst silent gap between data chunks (max_idle_ms — compare this
// against the configured idle timeout to judge how much margin healthy turns have).
func streamProgressAttrs(start, firstToken, lastDelta time.Time, streamBytes int, maxIdleMs int64) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("stream_bytes", streamBytes),
		slog.Int64("max_idle_ms", maxIdleMs),
	}
	if !firstToken.IsZero() {
		attrs = append(attrs, slog.Int64("first_token_ms", firstToken.Sub(start).Milliseconds()))
	}
	if !lastDelta.IsZero() {
		attrs = append(attrs, slog.Int64("since_last_delta_ms", time.Since(lastDelta).Milliseconds()))
	}
	return attrs
}

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

func (c *Client) StreamChatWithTools(ctx context.Context, messages []Message, tools []Tool, onEvent func(StreamEvent) error) (StreamResult, error) {
	start := time.Now()
	callCtx := ctx
	var cancel context.CancelFunc
	if timeout := c.timeoutForTools(tools); timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Idle watchdog: a model that stops emitting chunks mid-turn would otherwise
	// block on the read until the coarse total deadline (or a client disconnect).
	// Abort early when nothing arrives within the idle window; the timer is reset
	// on every received line (data chunk or keep-alive comment).
	streamCtx := callCtx
	resetIdle := func() {}
	if c.idleTimeout > 0 {
		var idleCancel context.CancelCauseFunc
		streamCtx, idleCancel = context.WithCancelCause(callCtx)
		defer idleCancel(nil)
		idleTimer := time.AfterFunc(c.idleTimeout, func() { idleCancel(ErrStreamStalled) })
		defer idleTimer.Stop()
		resetIdle = func() { idleTimer.Reset(c.idleTimeout) }
	}

	resp, err := c.executeChatRequestWithTools(streamCtx, messages, tools, true)
	if err != nil {
		// The watchdog can fire before the first byte arrives (upstream never
		// responds); report that as a stall rather than a raw context error.
		if errors.Is(context.Cause(streamCtx), ErrStreamStalled) {
			err = ErrStreamStalled
		}
		logInferenceFailed(streamCtx, c.model, time.Since(start), err)
		return StreamResult{}, err
	}
	defer resp.Body.Close()

	var content strings.Builder
	var reasoning strings.Builder
	var usage TokenUsage
	var finishReason string
	var firstTokenAt, lastDeltaAt time.Time
	streamBytes := 0
	// lastChunkAt/maxIdleMs track the worst silent gap between data chunks — the
	// window the idle watchdog actually races against. Seeded at start so the gap
	// before the first chunk (connect + time-to-first-byte) counts too.
	lastChunkAt := start
	var maxIdleMs int64
	noteChunk := func() {
		now := time.Now()
		if gap := now.Sub(lastChunkAt).Milliseconds(); gap > maxIdleMs {
			maxIdleMs = gap
		}
		lastChunkAt = now
		resetIdle()
	}
	noteDelta := func() {
		now := time.Now()
		if firstTokenAt.IsZero() {
			firstTokenAt = now
		}
		lastDeltaAt = now
	}
	progress := func() []slog.Attr {
		return streamProgressAttrs(start, firstTokenAt, lastDeltaAt, streamBytes, maxIdleMs)
	}
	fail := func(err error) (StreamResult, error) {
		logInferenceFailed(streamCtx, c.model, time.Since(start), err, progress()...)
		return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
	}
	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int
	// MiMo emits tool calls as inline XML in the content; gate the streamed
	// deltas so the raw <tool_call> markup never reaches the client. Only do this
	// when tools are actually on offer: the tool-free final-answer call must keep
	// its content verbatim, otherwise a stray inline call would be stripped to an
	// empty (and therefore discarded) response.
	parseInlineTools := isMiMoModel(c.model) && len(tools) > 0
	var gate *toolCallStreamGate
	if parseInlineTools {
		gate = &toolCallStreamGate{}
	}
	// Emitted at most once per turn, the moment we first know a tool call is
	// underway — well before the parsed call surfaces at the end of the stream.
	toolPendingEmitted := false
	flushGate := func() error {
		if gate == nil || onEvent == nil {
			return nil
		}
		if leftover := gate.flush(); leftover != "" {
			return onEvent(StreamEvent{Delta: leftover})
		}
		return nil
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		streamBytes += len(scanner.Bytes())
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			// Blank lines and SSE comments (": keep-alive") prove the connection is
			// alive but say nothing about the model making progress. Deliberately do
			// NOT reset the idle watchdog here, or a heartbeat-emitting upstream could
			// mask a stalled model; a dead connection is the total timeout's job.
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		// A data chunk is genuine stream progress from the model endpoint.
		noteChunk()

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			if err := flushGate(); err != nil {
				return fail(err)
			}
			result, err := finishStream(content.String(), reasoning.String(), finishReason, usage, toolCalls, toolCallOrder, onEvent, parseInlineTools)
			if err != nil {
				return fail(err)
			}
			result.Duration = time.Since(start)
			result.Model = c.model
			result.ReasoningEffort = c.reasoningEffort
			observeInference(streamCtx, c.model, result.Duration, result.Usage, result.FinishReason, progress()...)
			return result, nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fail(fmt.Errorf("decode chat completion chunk: %w", err))
		}
		if chunk.Usage.Present() {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
		delta := choice.Delta

		if delta.ReasoningContent != "" {
			noteDelta()
			reasoning.WriteString(delta.ReasoningContent)
			if onEvent != nil {
				if err := onEvent(StreamEvent{ReasoningDelta: delta.ReasoningContent}); err != nil {
					return fail(err)
				}
			}
		}
		if delta.Content != "" {
			noteDelta()
			content.WriteString(delta.Content)
			if onEvent != nil {
				emit := delta.Content
				if gate != nil {
					emit = gate.push(delta.Content)
				}
				if emit != "" {
					if err := onEvent(StreamEvent{Delta: emit}); err != nil {
						return fail(err)
					}
				}
				// The gate flips to suppressed the instant it sees the inline
				// <tool_call> marker; tell the client a tool call is coming.
				if gate != nil && gate.suppressed && !toolPendingEmitted {
					toolPendingEmitted = true
					if err := onEvent(StreamEvent{ToolPending: true}); err != nil {
						return fail(err)
					}
				}
			}
		}
		if len(delta.ToolCalls) > 0 {
			noteDelta()
			if !toolPendingEmitted && onEvent != nil {
				toolPendingEmitted = true
				if err := onEvent(StreamEvent{ToolPending: true}); err != nil {
					return fail(err)
				}
			}
		}
		for _, chunk := range delta.ToolCalls {
			call, ok := toolCalls[chunk.Index]
			if !ok {
				call = &ToolCall{Type: "function"}
				toolCalls[chunk.Index] = call
				toolCallOrder = append(toolCallOrder, chunk.Index)
			}
			if chunk.ID != "" {
				call.ID = chunk.ID
			}
			if chunk.Type != "" {
				call.Type = chunk.Type
			}
			if chunk.Function.Name != "" {
				call.Function.Name += chunk.Function.Name
			}
			if chunk.Function.Arguments != "" {
				call.Function.Arguments += chunk.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// The idle watchdog cancels streamCtx with ErrStreamStalled; surface that
		// sentinel (rather than the raw "context canceled" the read returns) so a
		// stalled upstream is distinguishable from a client disconnect.
		if errors.Is(context.Cause(streamCtx), ErrStreamStalled) {
			return fail(fmt.Errorf("read chat completion stream: %w", ErrStreamStalled))
		}
		return fail(fmt.Errorf("read chat completion stream: %w", err))
	}
	if err := flushGate(); err != nil {
		return fail(err)
	}
	result, err := finishStream(content.String(), reasoning.String(), finishReason, usage, toolCalls, toolCallOrder, onEvent, parseInlineTools)
	if err != nil {
		return fail(err)
	}
	result.Duration = time.Since(start)
	result.Model = c.model
	result.ReasoningEffort = c.reasoningEffort
	observeInference(streamCtx, c.model, result.Duration, result.Usage, result.FinishReason, progress()...)
	return result, nil
}

func finishStream(content string, reasoningContent string, finishReason string, usage TokenUsage, byIndex map[int]*ToolCall, order []int, onEvent func(StreamEvent) error, parseInlineTools bool) (StreamResult, error) {
	result := StreamResult{
		Content:          content,
		ReasoningContent: reasoningContent,
		FinishReason:     finishReason,
		ToolCalls:        make([]ToolCall, 0, len(order)),
		Usage:            usage,
	}
	for _, index := range order {
		call := *byIndex[index]
		result.ToolCalls = append(result.ToolCalls, call)
		if onEvent != nil {
			if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
				return result, err
			}
		}
	}
	// Models that lack native tool_calls (e.g. MiMo) emit the call as inline XML
	// in the content. Recover those only when the API returned no native calls.
	if parseInlineTools && len(result.ToolCalls) == 0 {
		inlineCalls, cleaned := parseInlineToolCalls(result.Content)
		for _, call := range inlineCalls {
			result.ToolCalls = append(result.ToolCalls, call)
			if onEvent != nil {
				if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
					return result, err
				}
			}
		}
		if len(inlineCalls) > 0 {
			result.Content = cleaned
		}
	}
	return result, nil
}
