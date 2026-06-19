package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

const (
	// maxToolRounds caps how many times the model may call tools before loom
	// forces a tool-free final answer. Kept moderate: a model that over-researches
	// (e.g. fetching source after source) otherwise burns rounds — and wall-clock —
	// without converging. Enough for genuine multi-step research, low enough to stop
	// a spiral.
	maxToolRounds             = 6
	maxToolCallsPerRound      = 8
	maxToolCallDuration       = 30 * time.Second
	maxToolResultContentBytes = 32 << 10
	toolFailedPrefix          = "tool failed"
)

type assistantLoopResult struct {
	llm.StreamResult
	Artifacts     []artifactResponse
	ToolError     string
	ActivityTrace []activityTraceEvent
	Blocks        []contentBlock
}

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, imageArtifactRequired bool) (assistantLoopResult, error) {
	tools := s.availableTools()
	if len(tools) == 0 {
		b := &blockBuilder{}
		result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), history, inferenceWithPurpose(inference, "chat", 1), nil)
		b.addResult(titles, result)
		if persistInterruptedPartial(result, err) {
			return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
		}
		return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
	}
	if imageArtifactRequired {
		if imageTool := findGenerateImageTool(tools); imageTool != nil {
			return s.runRequiredImageAssistantLoop(ctx, stream, titles, history, inference, user, thread, *imageTool)
		}
		slog.Warn("image artifact required but generate_image tool is unavailable", "thread_id", thread.ID, "tools", len(tools))
	}

	toolRan := false
	var artifacts []artifactResponse
	b := &blockBuilder{}
	for round := 1; round <= maxToolRounds; round++ {
		result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), history, inferenceWithPurpose(inference, "chat_tool_round", round), tools)
		if err != nil {
			if persistInterruptedPartial(result, err) {
				b.addResult(titles, result)
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
			}
			return assistantLoopResult{}, err
		}
		b.addResult(titles, result)
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response.
			if strings.TrimSpace(result.Content) != "" || !toolRan {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
			}
			slog.Info("forcing final answer", "reason", "empty_after_tools", "round", round)
			break
		}
		if len(result.ToolCalls) > maxToolCallsPerRound {
			return assistantLoopResult{}, streamUserError{message: fmt.Sprintf("too many tool calls in one assistant round: %d", len(result.ToolCalls))}
		}
		// Log every tool call's argument size so document payloads are measurable in
		// retrospect instead of guessed at: a create_*_file call serializes the whole
		// file into its argument JSON, so arg_bytes ≈ document size. Pair this with
		// completion_tokens from the matching "llm inference completed" line (same
		// thread_id + round) to read the size in tokens — the unit the completion-token
		// cap is set in. finish_reason=length means the argument was truncated, so
		// arg_bytes is then a lower bound on the intended size. Fires before the length
		// guard below so a truncated payload is still measured.
		for _, call := range result.ToolCalls {
			slog.Info("tool call arguments",
				"round", round,
				"tool", call.Function.Name,
				"arg_bytes", len(call.Function.Arguments),
				"finish_reason", result.FinishReason)
		}
		if result.FinishReason == "length" {
			// The model serializes a document as a single tool-call argument; once it
			// runs past the completion-token cap the argument JSON is truncated
			// mid-string. Appending that broken call to history and continuing makes
			// the upstream reject the next round's prefill (surfacing as a generic
			// "stream failed"), and the document tool itself cannot parse the partial
			// arguments. Stop here with a clear cause instead of replaying it.
			slog.Warn("tool call truncated at token cap",
				"round", round, "tool_calls", len(result.ToolCalls), "finish_reason", result.FinishReason)
			return assistantLoopResult{}, streamUserError{message: "The response was cut off before it finished — the requested output is too large to generate in one turn. Ask for a shorter version or split it into parts."}
		}
		slog.Info("assistant requested tools", "round", round, "tool_calls", len(result.ToolCalls), "content_bytes", len(result.Content))

		history = append(history, llm.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			output, response, handled := s.executeBuiltInTool(ctx, stream, user, thread, call)
			if handled {
				if response != nil {
					artifacts = append(artifacts, *response)
					b.addArtifact(*response)
				}
			} else {
				output = s.executeToolCall(ctx, user, call, round)
			}
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return assistantLoopResult{}, err
			}
			b.setToolResult(call.ID, output)
			history = append(history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    output,
			})
		}
		toolRan = true
		if round == maxToolRounds {
			slog.Info("forcing final answer", "reason", "rounds_exhausted", "round", round)
		}
	}
	// Force a final answer with no tools available, nudging the model to commit
	// to a reply using the gathered results instead of issuing yet another tool
	// call (which it cannot run and which would otherwise yield an empty turn).
	// A system directive (not a user turn) avoids biasing the answer's language.
	finalHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: "Provide your final answer now using the information already gathered above. Do not call any more tools.",
	})
	result, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), finalHistory, inferenceWithPurpose(inference, "chat_final", maxToolRounds+1), nil)
	b.addResult(titles, result)
	if persistInterruptedPartial(result, err) {
		return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}
	// MiMo regularly answers the forced final call with yet another inline tool call
	// instead of prose; the inline markup is stripped (so it never leaks), leaving the
	// content empty. Retry once with a firmer, tool-free directive, then fall back to a
	// fixed message — anything but persisting an empty (and therefore discarded) turn.
	if err == nil && strings.TrimSpace(result.Content) == "" {
		slog.Info("retrying empty final answer", "reason", "inline_tool_call_stripped", "round", maxToolRounds+1)
		retryHistory := append(history[:len(history):len(history)], llm.Message{
			Role:    "system",
			Content: "You have no tools available and must not emit any tool call. Answer the user's question now in plain prose, using only the information already gathered above.",
		})
		result, err = s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), retryHistory, inferenceWithPurpose(inference, "chat_final_retry", maxToolRounds+2), nil)
		b.addResult(titles, result)
		if persistInterruptedPartial(result, err) {
			return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
		}
		if err == nil && strings.TrimSpace(result.Content) == "" {
			slog.Warn("final answer empty after retry; using fallback", "round", maxToolRounds+2)
			result.Content = finalAnswerFallback
			// addResult skipped the empty turn's (blank) text; surface the fallback
			// prose as the final text block so the timeline matches the persisted
			// content column.
			b.addText(result.Content)
		}
	}
	return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
}

// finalAnswerFallback is surfaced when the model never commits to a prose answer on
// the forced final turn (it keeps emitting tool calls instead), so the turn shows a
// clear message rather than an empty bubble.
const finalAnswerFallback = "I couldn't put together a final answer from the information gathered. Please try rephrasing or narrowing your question."

func (s *server) runRequiredImageAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, imageTool llm.Tool) (assistantLoopResult, error) {
	compilerHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: imagePromptCompilerSystemPrompt,
	})
	b := &blockBuilder{}
	result, err := s.streamAssistantTurnSuppressingContent(ctx, stream, titles, b.nextReasoningID(), compilerHistory, inferenceWithPurpose(inference, "image_prompt_compiler", 1), []llm.Tool{imageTool})
	// The compiler turn's content is deliberately suppressed (it is the internal
	// prompt-compiler output, never shown), so add only its reasoning/tool events
	// — adding its prose would leak hidden text into the timeline.
	b.addTraceOnlyResult(titles, result)
	if err != nil {
		return assistantLoopResult{}, err
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Name != "generate_image" {
		return assistantLoopResult{StreamResult: result, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}

	call := result.ToolCalls[0]
	history = append(compilerHistory, llm.Message{
		Role:      "assistant",
		ToolCalls: result.ToolCalls,
	})
	output, response, handled := s.executeBuiltInTool(ctx, stream, user, thread, call)
	if !handled {
		output = capToolOutput("tool failed: generate_image is not available")
	}
	if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
		return assistantLoopResult{}, err
	}
	b.setToolResult(call.ID, output)
	history = append(history, llm.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Content:    output,
	})
	if response == nil {
		return assistantLoopResult{StreamResult: result, ToolError: output, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}

	b.addArtifact(*response)
	artifacts := []artifactResponse{*response}
	finalHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: "Provide a brief final response that refers to the created artifact. Do not call any more tools. Never claim an image was created unless the tool result confirms an artifact.",
	})
	final, err := s.streamAssistantTurn(ctx, stream, titles, b.nextReasoningID(), finalHistory, inferenceWithPurpose(inference, "image_final", 2), nil)
	b.addResult(titles, final)
	if persistInterruptedPartial(final, err) {
		return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, nil
	}
	if err == nil && strings.TrimSpace(final.Content) == "" {
		final.Content = fallbackImageArtifactResponse(*response)
		// addResult skipped the empty final turn's text; surface the fallback prose
		// so the timeline matches the persisted content column.
		b.addText(final.Content)
	}
	return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: b.flatTrace(), Blocks: b.blocks}, err
}

func fallbackImageArtifactResponse(response artifactResponse) string {
	if strings.TrimSpace(response.DisplayFilename) == "" {
		return "Created the image artifact."
	}
	return "Created " + response.DisplayFilename + "."
}

// persistInterruptedPartial reports whether a turn that ended in an interruption —
// a client disconnect (context.Canceled) or a stalled upstream
// (llm.ErrStreamStalled) — still produced partial content worth keeping. Without
// this, a stall after some content streamed would discard the whole turn.
// Reasoning-only output is not persistable on its own.
func persistInterruptedPartial(result llm.StreamResult, err error) bool {
	if strings.TrimSpace(result.Content) == "" {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, llm.ErrStreamStalled)
}

// streamAssistantTurn runs one model turn, relaying reasoning/content deltas and
// tool-call events to the SSE stream. titles/reasoningID let it spawn the
// reasoning abstract the instant the model stops reasoning and starts answering
// (or calling a tool), so the title overlaps the answer stream instead of
// waiting for the turn to finish.
func (s *server) streamAssistantTurn(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return s.streamAssistantTurnWithContentStreaming(ctx, stream, titles, reasoningID, history, meta, tools, true)
}

func (s *server) streamAssistantTurnSuppressingContent(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	return s.streamAssistantTurnWithContentStreaming(ctx, stream, titles, reasoningID, history, meta, tools, false)
}

func (s *server) streamAssistantTurnWithContentStreaming(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, reasoningID string, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool, streamContent bool) (llm.StreamResult, error) {
	callCtx := llm.WithInferenceMetadata(ctx, meta)
	var reasoningBuf strings.Builder
	titleSpawned := false
	// The reasoning->content (or reasoning->tool) boundary: the model has
	// finished thinking, so the round's reasoning is complete and its title can
	// generate while the answer streams.
	spawnTitle := func() {
		if titleSpawned {
			return
		}
		titleSpawned = true
		titles.spawn(reasoningID, reasoningBuf.String())
	}
	return s.llm.StreamChatWithTools(callCtx, history, tools, func(event llm.StreamEvent) error {
		if event.ReasoningDelta != "" {
			reasoningBuf.WriteString(event.ReasoningDelta)
			return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
		}
		if event.ToolPending {
			spawnTitle()
			return sendSSEJSON(stream, "tool_pending", struct{}{})
		}
		if event.Delta != "" && streamContent {
			spawnTitle()
			return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
		}
		if event.ToolCall.ID != "" || event.ToolCall.Function.Name != "" {
			spawnTitle()
			return sendSSEJSON(stream, "tool_call", toolCallResponse{
				ID:        event.ToolCall.ID,
				Name:      event.ToolCall.Function.Name,
				Arguments: event.ToolCall.Function.Arguments,
			})
		}
		return nil
	})
}

func inferenceWithPurpose(metadata llm.InferenceMetadata, purpose string, round int) llm.InferenceMetadata {
	metadata.Purpose = purpose
	metadata.Round = round
	return metadata
}
