package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/sse"
)

const sparkSystemPrompt = "You are Spark, a concise assistant for work and school. Match the language of the user's most recent message. When that message is too short or ambiguous to determine a language (for example a single name, number, or symbol), respond in English. Ignore the language of any tool results or retrieved documents when choosing your reply language. This holds unless their profile requests a specific response language."

func (s *server) handleStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body streamMessageRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	threadID := r.PathValue("threadID")
	thread, found, err := s.chat.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	priorMessages, found, err := s.chat.ListMessages(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list messages failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	userMessage, err := s.chat.AddMessage(r.Context(), user.ID, threadID, chat.RoleUser, body.Content)
	if err != nil {
		writeChatStoreError(w, err, http.StatusBadRequest, "message content is required", "message content is too long")
		return
	}

	stream, err := sse.NewWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := sendSSEJSON(stream, "user_message", userMessage); err != nil {
		return
	}

	// Kick off the MCP reachability probe now so its latency overlaps with
	// assistant generation instead of delaying the final events.
	mcpStatusCh := s.startMCPStatus(r.Context())

	if thread.Title == chat.DefaultThreadTitle {
		_ = s.generateAndSendThreadTitle(r.Context(), context.WithoutCancel(r.Context()), stream, user, threadID, userMessage.Content, "")
	}

	history := buildLLMHistory(user, priorMessages, userMessage)
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID}
	assistantResult, err := s.runAssistantLoop(r.Context(), stream, history, inference)
	if err != nil {
		message := "stream failed"
		var userErr streamUserError
		if errors.As(err, &userErr) {
			message = userErr.message
		}
		_ = sendSSEJSON(stream, "error", map[string]string{"error": message})
		return
	}
	assistantContent := assistantResult.Content
	if strings.TrimSpace(assistantContent) == "" {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "empty assistant response"})
		return
	}

	persistCtx := context.WithoutCancel(r.Context())
	assistantMessage, err := s.chat.AddMessageWithUsage(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageMetricsFromResult(assistantResult))
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		return
	}
	if err := sendSSEJSON(stream, "assistant_message", assistantMessage); err != nil {
		return
	}

	sendMCPStatus(stream, mcpStatusCh)
	_ = stream.Send("done", "{}")
}

const (
	maxToolRounds             = 4
	maxToolCallsPerRound      = 8
	maxToolCallDuration       = 30 * time.Second
	maxToolResultContentBytes = 32 << 10
)

type streamUserError struct {
	message string
}

func (e streamUserError) Error() string {
	return e.message
}

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, history []llm.Message, inference llm.InferenceMetadata) (llm.StreamResult, error) {
	tools := []llm.Tool(nil)
	if s.mcp != nil {
		tools = s.mcp.Tools()
	}
	if len(tools) == 0 {
		callCtx := llm.WithInferenceMetadata(ctx, inferenceWithPurpose(inference, "chat", 1))
		return s.llm.StreamChatWithTools(callCtx, history, nil, func(event llm.StreamEvent) error {
			if event.ReasoningDelta != "" {
				return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
			}
			if event.Delta != "" {
				return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
			}
			return nil
		})
	}

	for round := 1; round <= maxToolRounds; round++ {
		callCtx := llm.WithInferenceMetadata(ctx, inferenceWithPurpose(inference, "chat_tool_round", round))
		result, err := s.llm.StreamChatWithTools(callCtx, history, tools, func(event llm.StreamEvent) error {
			if event.ReasoningDelta != "" {
				return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
			}
			if event.Delta != "" {
				return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
			}
			if event.ToolCall.ID != "" || event.ToolCall.Function.Name != "" {
				return sendSSEJSON(stream, "tool_call", toolCallResponse{
					ID:        event.ToolCall.ID,
					Name:      event.ToolCall.Function.Name,
					Arguments: event.ToolCall.Function.Arguments,
				})
			}
			return nil
		})
		if err != nil {
			return llm.StreamResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			return result, nil
		}
		if len(result.ToolCalls) > maxToolCallsPerRound {
			return llm.StreamResult{}, streamUserError{message: fmt.Sprintf("too many tool calls in one assistant round: %d", len(result.ToolCalls))}
		}

		history = append(history, llm.Message{
			Role:             "assistant",
			Content:          result.Content,
			ReasoningContent: result.ReasoningContent,
			ToolCalls:        result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			output := s.executeToolCall(ctx, call)
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return llm.StreamResult{}, err
			}
			history = append(history, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    output,
			})
		}
	}
	callCtx := llm.WithInferenceMetadata(ctx, inferenceWithPurpose(inference, "chat_final", maxToolRounds+1))
	return s.llm.StreamChatWithTools(callCtx, history, nil, func(event llm.StreamEvent) error {
		if event.ReasoningDelta != "" {
			return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
		}
		if event.Delta != "" {
			return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
		}
		return nil
	})
}

func (s *server) executeToolCall(ctx context.Context, call llm.ToolCall) string {
	arguments, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: " + err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, maxToolCallDuration)
	defer cancel()
	output, err := s.mcp.CallTool(callCtx, call.Function.Name, arguments)
	if err != nil {
		return capToolOutput("tool failed: " + err.Error())
	}
	return capToolOutput(output)
}

func capToolOutput(output string) string {
	if len(output) <= maxToolResultContentBytes {
		return output
	}
	return output[:maxToolResultContentBytes]
}

func inferenceWithPurpose(metadata llm.InferenceMetadata, purpose string, round int) llm.InferenceMetadata {
	metadata.Purpose = purpose
	metadata.Round = round
	return metadata
}

func messageMetricsFromResult(result llm.StreamResult) chat.MessageTokenUsage {
	metrics := chat.MessageTokenUsage{ReasoningContent: result.ReasoningContent}
	if result.Model != "" {
		metrics.Model = strPtr(result.Model)
	}
	if result.ReasoningEffort != "" {
		metrics.ReasoningEffort = strPtr(result.ReasoningEffort)
	}
	if result.Duration > 0 {
		metrics.DurationMs = intPtr(int(result.Duration.Milliseconds()))
	}
	if result.Usage.Present() {
		metrics.PromptTokens = intPtr(result.Usage.PromptTokens)
		metrics.CompletionTokens = intPtr(result.Usage.CompletionTokens)
		metrics.TotalTokens = intPtr(result.Usage.TotalTokens)
		metrics.CachedTokens = intPtr(result.Usage.PromptTokensDetails.CachedTokens)
		metrics.ReasoningTokens = intPtr(result.Usage.CompletionTokenDetails.ReasoningTokens)
	}
	return metrics
}

func intPtr(value int) *int {
	return &value
}

func strPtr(value string) *string {
	return &value
}

func parseToolArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

func (s *server) generateAndSendThreadTitle(requestCtx, persistCtx context.Context, stream *sse.Writer, user auth.User, threadID, userMessage, assistantMessage string) error {
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID, Purpose: "title", Round: 1}
	title, err := s.llm.GenerateTitle(llm.WithInferenceMetadata(requestCtx, inference), userMessage, assistantMessage)
	if err != nil {
		return err
	}
	thread, found, err := s.chat.UpdateThread(persistCtx, user.ID, threadID, chat.UpdateThreadInput{Title: &title})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return sendSSEJSON(stream, "thread", thread)
}

// startMCPStatus begins a live reachability probe of the configured MCP servers
// in the background so its latency overlaps with assistant generation. It
// returns nil when MCP is disabled. The channel is buffered, so the probe never
// leaks even if the caller returns early without reading it.
func (s *server) startMCPStatus(ctx context.Context) <-chan mcpStatusResponse {
	if s.mcp == nil {
		return nil
	}
	ch := make(chan mcpStatusResponse, 1)
	go func() {
		ch <- s.currentMCPStatus(ctx)
	}()
	return ch
}

// sendMCPStatus emits a best-effort mcp_status event from a probe started by
// startMCPStatus. It is skipped when MCP is disabled or no servers are
// configured, and never aborts the stream.
func sendMCPStatus(stream *sse.Writer, ch <-chan mcpStatusResponse) {
	if ch == nil {
		return
	}
	status := <-ch
	if status.Configured == 0 {
		return
	}
	_ = sendSSEJSON(stream, "mcp_status", status)
}

func buildLLMHistory(user auth.User, messages []chat.Message, newUserMessage chat.Message) []llm.Message {
	history := []llm.Message{{Role: "system", Content: systemPromptForUser(user)}}
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser, chat.RoleAssistant:
			history = append(history, llm.Message{
				Role:             string(message.Role),
				Content:          message.Content,
				ReasoningContent: message.ReasoningContent,
			})
		}
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage.Content})
	return history
}

func systemPromptForUser(user auth.User) string {
	if user.ResponseLanguage == "" || strings.EqualFold(user.ResponseLanguage, "auto") {
		return sparkSystemPrompt
	}
	return sparkSystemPrompt + "\nAlways answer in this language: " + user.ResponseLanguage + "."
}

func sendSSEJSON(stream *sse.Writer, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return stream.Send(event, string(payload))
}
