package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/sse"
)

const (
	maxToolRounds             = 8
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
}

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, imageArtifactRequired bool) (assistantLoopResult, error) {
	tools := s.availableTools()
	if len(tools) == 0 {
		result, err := s.streamAssistantTurn(ctx, stream, titles, nextReasoningID(nil), history, inferenceWithPurpose(inference, "chat", 1), nil)
		if persistCanceledPartial(result, err) {
			return assistantLoopResult{StreamResult: result, ActivityTrace: s.appendTraceAndSpawnTitle(titles, nil, result)}, nil
		}
		return assistantLoopResult{StreamResult: result, ActivityTrace: s.appendTraceAndSpawnTitle(titles, nil, result)}, err
	}
	if imageArtifactRequired {
		if imageTool := findGenerateImageTool(tools); imageTool != nil {
			return s.runRequiredImageAssistantLoop(ctx, stream, titles, history, inference, user, thread, *imageTool)
		}
		slog.Warn("image artifact required but generate_image tool is unavailable", "thread_id", thread.ID, "tools", len(tools))
	}

	toolRan := false
	var artifacts []artifactResponse
	var trace []activityTraceEvent
	for round := 1; round <= maxToolRounds; round++ {
		result, err := s.streamAssistantTurn(ctx, stream, titles, nextReasoningID(trace), history, inferenceWithPurpose(inference, "chat_tool_round", round), tools)
		if err != nil {
			if persistCanceledPartial(result, err) {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: s.appendTraceAndSpawnTitle(titles, trace, result)}, nil
			}
			return assistantLoopResult{}, err
		}
		trace = s.appendTraceAndSpawnTitle(titles, trace, result)
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response.
			if strings.TrimSpace(result.Content) != "" || !toolRan {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: trace}, nil
			}
			slog.Info("forcing final answer", "reason", "empty_after_tools", "round", round)
			break
		}
		if len(result.ToolCalls) > maxToolCallsPerRound {
			return assistantLoopResult{}, streamUserError{message: fmt.Sprintf("too many tool calls in one assistant round: %d", len(result.ToolCalls))}
		}
		slog.Info("assistant requested tools", "round", round, "tool_calls", len(result.ToolCalls), "content_bytes", len(result.Content))

		history = append(history, llm.Message{
			Role:             "assistant",
			Content:          result.Content,
			ReasoningContent: result.ReasoningContent,
			ToolCalls:        result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			output, response, handled := s.executeBuiltInTool(ctx, stream, user, thread, call)
			if handled {
				if response != nil {
					artifacts = append(artifacts, *response)
				}
			} else {
				output = s.executeToolCall(ctx, call, round)
			}
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return assistantLoopResult{}, err
			}
			trace = activityTraceWithToolResult(trace, call.ID, output)
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
	result, err := s.streamAssistantTurn(ctx, stream, titles, nextReasoningID(trace), finalHistory, inferenceWithPurpose(inference, "chat_final", maxToolRounds+1), nil)
	trace = s.appendTraceAndSpawnTitle(titles, trace, result)
	if persistCanceledPartial(result, err) {
		return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: trace}, nil
	}
	return assistantLoopResult{StreamResult: result, Artifacts: artifacts, ActivityTrace: trace}, err
}

func (s *server) runRequiredImageAssistantLoop(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread, imageTool llm.Tool) (assistantLoopResult, error) {
	compilerHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: imagePromptCompilerSystemPrompt,
	})
	result, err := s.streamAssistantTurnSuppressingContent(ctx, stream, titles, nextReasoningID(nil), compilerHistory, inferenceWithPurpose(inference, "image_prompt_compiler", 1), []llm.Tool{imageTool})
	trace := s.appendTraceAndSpawnTitle(titles, nil, result)
	if err != nil {
		return assistantLoopResult{}, err
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Name != "generate_image" {
		return assistantLoopResult{StreamResult: result, ActivityTrace: trace}, nil
	}

	call := result.ToolCalls[0]
	history = append(compilerHistory, llm.Message{
		Role:             "assistant",
		ReasoningContent: result.ReasoningContent,
		ToolCalls:        result.ToolCalls,
	})
	output, response, handled := s.executeBuiltInTool(ctx, stream, user, thread, call)
	if !handled {
		output = capToolOutput("tool failed: generate_image is not available")
	}
	if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
		return assistantLoopResult{}, err
	}
	trace = activityTraceWithToolResult(trace, call.ID, output)
	history = append(history, llm.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Content:    output,
	})
	if response == nil {
		return assistantLoopResult{StreamResult: result, ToolError: output, ActivityTrace: trace}, nil
	}

	artifacts := []artifactResponse{*response}
	finalHistory := append(history[:len(history):len(history)], llm.Message{
		Role:    "system",
		Content: "Provide a brief final response that refers to the created artifact. Do not call any more tools. Never claim an image was created unless the tool result confirms an artifact.",
	})
	final, err := s.streamAssistantTurn(ctx, stream, titles, nextReasoningID(trace), finalHistory, inferenceWithPurpose(inference, "image_final", 2), nil)
	trace = s.appendTraceAndSpawnTitle(titles, trace, final)
	if persistCanceledPartial(final, err) {
		return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: trace}, nil
	}
	if err == nil && strings.TrimSpace(final.Content) == "" {
		final.Content = fallbackImageArtifactResponse(*response)
	}
	return assistantLoopResult{StreamResult: final, Artifacts: artifacts, ActivityTrace: trace}, err
}

func fallbackImageArtifactResponse(response artifactResponse) string {
	if strings.TrimSpace(response.DisplayFilename) == "" {
		return "Created the image artifact."
	}
	return "Created " + response.DisplayFilename + "."
}

func persistCanceledPartial(result llm.StreamResult, err error) bool {
	return errors.Is(err, context.Canceled) && strings.TrimSpace(result.Content) != ""
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
