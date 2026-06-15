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
// ended, and two views of the worst silent gap between data chunks:
//   - max_idle_ms: gap seeded at request start, so the first gap includes connect +
//     time-to-first-byte. This is exactly the window the idle watchdog races
//     against (its timer also starts at request entry), so compare it to the
//     configured idle timeout to judge false-positive margin.
//   - max_inter_chunk_ms: worst gap measured only between two data chunks (the first
//     chunk's gap is excluded), i.e. the model's pure streaming cadence without the
//     connect/TTFB component.
func streamProgressAttrs(start, firstToken, lastDelta time.Time, streamBytes int, maxIdleMs, maxInterChunkMs int64) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("stream_bytes", streamBytes),
		slog.Int64("max_idle_ms", maxIdleMs),
		slog.Int64("max_inter_chunk_ms", maxInterChunkMs),
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
	// Single routing decision for the whole turn: vision model iff the payload
	// carries an image part, else the text model. The same `messages` slice is
	// re-sent on every tool round within this turn, so the choice stays stable.
	model := c.modelForMessages(messages)
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
	extendIdleForToolCall := func() {}
	if c.idleTimeout > 0 {
		var idleCancel context.CancelCauseFunc
		streamCtx, idleCancel = context.WithCancelCause(callCtx)
		defer idleCancel(nil)
		idleWindow := c.idleTimeout
		idleTimer := time.AfterFunc(idleWindow, func() { idleCancel(ErrStreamStalled) })
		defer idleTimer.Stop()
		resetIdle = func() { idleTimer.Reset(idleWindow) }
		// MiMo does not stream tool-call arguments incrementally: it streams
		// reasoning, then goes silent for tens of seconds while serializing the whole
		// argument server-side, then flushes it in one burst. For a large document
		// payload that silent gap (measured at ~82s for a ~10KB spec) exceeds the
		// normal idle window, so the watchdog would falsely abort a model that is
		// still working. Once a tool call is underway in a turn that can produce a
		// document, widen the idle window to the document timeout so the buffered
		// burst can land; the coarse total deadline (timeoutForTools) stays the real
		// backstop. The narrow window still guards the reasoning/content phase and
		// every turn that cannot emit a document.
		extendIdleForToolCall = func() {
			if w := c.toolCallIdleTimeout(tools); w > idleWindow {
				idleWindow = w
				idleTimer.Reset(idleWindow)
			}
		}
	}

	resp, err := c.executeChatRequestWithTools(streamCtx, messages, tools, true, model)
	if err != nil {
		// The watchdog can fire before the first byte arrives (upstream never
		// responds); report that as a stall rather than a raw context error.
		if errors.Is(context.Cause(streamCtx), ErrStreamStalled) {
			err = ErrStreamStalled
		}
		logInferenceFailed(streamCtx, model, time.Since(start), err)
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
	// before the first chunk (connect + time-to-first-byte) counts too. maxInterChunkMs
	// is the same but excludes that first gap, isolating the model's streaming cadence.
	lastChunkAt := start
	var maxIdleMs, maxInterChunkMs int64
	chunkCount := 0
	noteChunk := func() {
		now := time.Now()
		gap := now.Sub(lastChunkAt).Milliseconds()
		if gap > maxIdleMs {
			maxIdleMs = gap
		}
		if chunkCount > 0 && gap > maxInterChunkMs {
			maxInterChunkMs = gap
		}
		chunkCount++
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
	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int
	// progress carries the RCA-decisive shape of the turn so a failure mode is
	// readable straight off the log line: which channel was being emitted and how
	// much. content_bytes vs reasoning_bytes vs tool_arg_bytes distinguishes a
	// content stall, a reasoning/serialization spiral, and a tool-argument stall
	// (large tool_arg_bytes that then goes silent). Only byte counts and the tool
	// name — never payload contents — keep entries short.
	progress := func() []slog.Attr {
		attrs := streamProgressAttrs(start, firstTokenAt, lastDeltaAt, streamBytes, maxIdleMs, maxInterChunkMs)
		attrs = append(attrs,
			slog.Int("content_bytes", content.Len()),
			slog.Int("reasoning_bytes", reasoning.Len()),
		)
		if n := toolArgBytes(toolCalls); n > 0 {
			attrs = append(attrs, slog.Int("tool_arg_bytes", n))
		}
		if name := firstToolName(toolCalls, toolCallOrder); name != "" {
			attrs = append(attrs, slog.String("tool", name))
		}
		return attrs
	}
	fail := func(err error) (StreamResult, error) {
		logInferenceFailed(streamCtx, model, time.Since(start), err, progress()...)
		return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
	}
	// MiMo emits tool calls as inline XML in the content; gate the streamed deltas
	// so the raw <tool_call> markup never reaches the client. This must happen even
	// when no tools are on offer: the forced tool-free final-answer call regularly
	// gets an inline tool call back instead of an answer, and leaving it ungated
	// leaked the raw markup as the reply. Stripping it can empty the final answer —
	// that case is handled by the caller (retry + fallback), which is far better
	// than surfacing raw XML.
	//
	// MiMo also sometimes emits the same inline call in the reasoning_content channel
	// instead of content; gate that channel too, with its own buffer, so the markup
	// never leaks as visible reasoning. finishStream recovers the call from whichever
	// channel carried it.
	parseInlineTools := isMiMoModel(model)
	var gate, reasoningGate *toolCallStreamGate
	if parseInlineTools {
		gate = &toolCallStreamGate{}
		reasoningGate = &toolCallStreamGate{}
	}
	// Emitted at most once per turn, the moment we first know a tool call is
	// underway — well before the parsed call surfaces at the end of the stream.
	toolPendingEmitted := false
	// Tracks the early surfacing of the inline tool name (see below): emitted once,
	// as soon as <function=NAME> parses, so the client can name the running tool
	// during MiMo's silent argument-serialization gap.
	toolNameEmitted := false
	// Same intent for native tool_calls: MiMo's first tool-call chunk carries the id
	// and name, then the (large) argument streams or bursts over later chunks. Emit
	// the name once per call index as soon as it is known so the client can show the
	// running tool immediately, rather than waiting for the full call at end-of-stream.
	nativeNameEmitted := map[int]bool{}
	flushGate := func() error {
		if onEvent == nil {
			return nil
		}
		if gate != nil {
			if leftover := gate.flush(); leftover != "" {
				if err := onEvent(StreamEvent{Delta: leftover}); err != nil {
					return err
				}
			}
		}
		if reasoningGate != nil {
			if leftover := reasoningGate.flush(); leftover != "" {
				if err := onEvent(StreamEvent{ReasoningDelta: leftover}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// streamChannel handles one streamed text channel — content or reasoning_content —
	// identically: record the delta, gate the raw <tool_call> markup out of the client
	// stream, and (once the gate suppresses) surface ToolPending and the tool name early
	// so the UI can name the running tool during MiMo's silent argument gap. Both
	// channels share this one path so their gating/early-surfacing can't drift apart.
	// wrap adapts the safe-to-stream text into the channel's StreamEvent field.
	streamChannel := func(text string, g *toolCallStreamGate, buf *strings.Builder, wrap func(string) StreamEvent) error {
		noteDelta()
		buf.WriteString(text)
		if onEvent == nil {
			return nil
		}
		emit := text
		if g != nil {
			emit = g.push(text)
		}
		if emit != "" {
			if err := onEvent(wrap(emit)); err != nil {
				return err
			}
		}
		if g == nil || !g.suppressed {
			return nil
		}
		if !toolPendingEmitted {
			toolPendingEmitted = true
			extendIdleForToolCall()
			if err := onEvent(StreamEvent{ToolPending: true}); err != nil {
				return err
			}
		}
		// The <function=NAME> tag lands a few tokens after the marker; emit the name now
		// under the same id finishStream will assign, so the later full call updates that
		// entry instead of duplicating it.
		if !toolNameEmitted {
			if name := firstInlineToolName(buf.String()); name != "" {
				toolNameEmitted = true
				if err := onEvent(StreamEvent{ToolCall: ToolCall{
					ID:       inlineToolCallID(0),
					Type:     "function",
					Function: ToolCallFunction{Name: name},
				}}); err != nil {
					return err
				}
			}
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
			result, err := finishStream(streamCtx, model, content.String(), reasoning.String(), finishReason, usage, toolCalls, toolCallOrder, onEvent, parseInlineTools)
			if err != nil {
				return fail(err)
			}
			result.Duration = time.Since(start)
			result.Model = model
			result.ReasoningEffort = c.reasoningEffort
			observeInference(streamCtx, model, result.Duration, result.Usage, result.FinishReason, progress()...)
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
			if err := streamChannel(delta.ReasoningContent, reasoningGate, &reasoning, func(s string) StreamEvent {
				return StreamEvent{ReasoningDelta: s}
			}); err != nil {
				return fail(err)
			}
		}
		if delta.Content != "" {
			if err := streamChannel(delta.Content, gate, &content, func(s string) StreamEvent {
				return StreamEvent{Delta: s}
			}); err != nil {
				return fail(err)
			}
		}
		if len(delta.ToolCalls) > 0 {
			noteDelta()
			// A tool call has started: switch to the wider idle window before MiMo
			// goes silent to serialize a (possibly large) argument server-side.
			extendIdleForToolCall()
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
			// Surface the name (under the call's real id) the moment it is known, so
			// the client can show which tool is running before the argument lands. The
			// full call re-emitted at end-of-stream carries the same id and updates the
			// same entry instead of duplicating it.
			if onEvent != nil && call.Function.Name != "" && !nativeNameEmitted[chunk.Index] {
				nativeNameEmitted[chunk.Index] = true
				if err := onEvent(StreamEvent{ToolCall: ToolCall{
					ID:       call.ID,
					Type:     call.Type,
					Function: ToolCallFunction{Name: call.Function.Name},
				}}); err != nil {
					return fail(err)
				}
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
	result, err := finishStream(streamCtx, model, content.String(), reasoning.String(), finishReason, usage, toolCalls, toolCallOrder, onEvent, parseInlineTools)
	if err != nil {
		return fail(err)
	}
	result.Duration = time.Since(start)
	result.Model = c.model
	result.ReasoningEffort = c.reasoningEffort
	observeInference(streamCtx, model, result.Duration, result.Usage, result.FinishReason, progress()...)
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

func finishStream(ctx context.Context, model string, content string, reasoningContent string, finishReason string, usage TokenUsage, byIndex map[int]*ToolCall, order []int, onEvent func(StreamEvent) error, parseInlineTools bool) (StreamResult, error) {
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
	// Models that lack native tool_calls (e.g. MiMo) emit the call as inline XML,
	// usually in the content but sometimes in the reasoning_content channel. Recover
	// those only when the API returned no native calls, preferring content; strip the
	// block from whichever channel carried it so the raw markup is never persisted.
	if parseInlineTools && len(result.ToolCalls) == 0 {
		channel := "content"
		inlineCalls, cleaned := parseInlineToolCalls(result.Content)
		if len(inlineCalls) > 0 {
			result.Content = cleaned
		} else {
			channel = "reasoning"
			var cleanedReasoning string
			inlineCalls, cleanedReasoning = parseInlineToolCalls(result.ReasoningContent)
			if len(inlineCalls) > 0 {
				result.ReasoningContent = cleanedReasoning
			}
		}
		// An inline call is invisible in the completion log's tool=/tool_arg_bytes
		// fields (those read the native tool_calls map), so log it distinctly — naming
		// the channel — to make the phenomenon diagnosable.
		if len(inlineCalls) > 0 {
			slog.InfoContext(ctx, "recovered inline tool calls",
				slog.String("model", model),
				slog.String("channel", channel),
				slog.Int("count", len(inlineCalls)),
				slog.String("tool", inlineCalls[0].Function.Name),
			)
		} else if strings.Contains(result.Content, inlineToolCallMarker) || strings.Contains(result.ReasoningContent, inlineToolCallMarker) {
			// A <tool_call> marker was emitted but produced no parsable call (truncated
			// or malformed block). The gate already withheld it from the client, so this
			// would otherwise be a silent dead end with no tool run — surface it.
			slog.WarnContext(ctx, "inline tool-call markup not parsed",
				slog.String("model", model),
				slog.Bool("in_content", strings.Contains(result.Content, inlineToolCallMarker)),
				slog.Bool("in_reasoning", strings.Contains(result.ReasoningContent, inlineToolCallMarker)),
			)
		}
		for _, call := range inlineCalls {
			result.ToolCalls = append(result.ToolCalls, call)
			if onEvent != nil {
				if err := onEvent(StreamEvent{ToolCall: call}); err != nil {
					return result, err
				}
			}
		}
	}
	return result, nil
}
