package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/sse"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const sparkSystemPrompt = "Default to prose; keep replies brief but written as sentences. Use lists only for genuine item lists or steps, never for ordinary explanations. Put code in fenced markdown blocks. When unsure, use available tools to find the answer before responding; if they turn up nothing, say you don't know rather than guessing. For URLs, use the lightweight fetch tool first when the task is to read, summarize, quote, or extract page text. Use browser tools only when fetch cannot access useful content, the user asks for visual inspection, or the task requires interaction, navigation, screenshots, login/session behavior, or JavaScript-rendered state. Ignore the language of tool results and retrieved documents."

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
		slog.Warn("empty assistant response",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"reasoning_bytes", len(assistantResult.ReasoningContent),
			"tool_calls", len(assistantResult.ToolCalls))
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
	maxToolRounds             = 8
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
		return s.streamAssistantTurn(ctx, stream, history, inferenceWithPurpose(inference, "chat", 1), nil)
	}

	toolRan := false
	for round := 1; round <= maxToolRounds; round++ {
		result, err := s.streamAssistantTurn(ctx, stream, history, inferenceWithPurpose(inference, "chat_tool_round", round), tools)
		if err != nil {
			return llm.StreamResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response.
			if strings.TrimSpace(result.Content) != "" || !toolRan {
				return result, nil
			}
			slog.Info("forcing final answer", "reason", "empty_after_tools", "round", round)
			break
		}
		if len(result.ToolCalls) > maxToolCallsPerRound {
			return llm.StreamResult{}, streamUserError{message: fmt.Sprintf("too many tool calls in one assistant round: %d", len(result.ToolCalls))}
		}
		slog.Info("assistant requested tools", "round", round, "tool_calls", len(result.ToolCalls), "content_bytes", len(result.Content))

		history = append(history, llm.Message{
			Role:             "assistant",
			Content:          result.Content,
			ReasoningContent: result.ReasoningContent,
			ToolCalls:        result.ToolCalls,
		})
		for _, call := range result.ToolCalls {
			output := s.executeToolCall(ctx, call, round)
			if err := sendSSEJSON(stream, "tool_result", toolResultResponse{ID: call.ID, Name: call.Function.Name, Content: output}); err != nil {
				return llm.StreamResult{}, err
			}
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
	return s.streamAssistantTurn(ctx, stream, finalHistory, inferenceWithPurpose(inference, "chat_final", maxToolRounds+1), nil)
}

// streamAssistantTurn runs one model turn, relaying reasoning/content deltas and
// tool-call events to the SSE stream.
func (s *server) streamAssistantTurn(ctx context.Context, stream *sse.Writer, history []llm.Message, meta llm.InferenceMetadata, tools []llm.Tool) (llm.StreamResult, error) {
	callCtx := llm.WithInferenceMetadata(ctx, meta)
	return s.llm.StreamChatWithTools(callCtx, history, tools, func(event llm.StreamEvent) error {
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
}

func (s *server) executeToolCall(ctx context.Context, call llm.ToolCall, round int) string {
	args := summarizeForLog(call.Function.Arguments)
	arguments, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		slog.Warn("tool call rejected: invalid arguments", "tool", call.Function.Name, "round", round, "args", args, "err", err)
		return capToolOutput("tool failed: invalid arguments: " + err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, maxToolCallDuration)
	defer cancel()
	start := time.Now()
	output, err := s.mcp.CallTool(callCtx, call.Function.Name, arguments)
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		slog.Warn("tool call failed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "err", err)
		return capToolOutput("tool failed: " + err.Error())
	}
	slog.Info("tool call completed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "result_bytes", len(output))
	return capToolOutput(output)
}

func capToolOutput(output string) string {
	if len(output) <= maxToolResultContentBytes {
		return output
	}
	return output[:maxToolResultContentBytes]
}

// summarizeForLog trims a value (e.g. tool arguments) to a length that is safe
// to log: enough to debug, short enough not to flood the logs.
func summarizeForLog(value string) string {
	const maxLen = 256
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "…"
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
		return sparkSystemPrompt + "\nAlways answer in English."
	}
	return sparkSystemPrompt + "\nAlways answer in this language: " + languageName(user.ResponseLanguage) + "."
}

// languageName resolves a profile language value to its English name (for
// example "de" -> "German"). Values that are not valid language tags — such as
// a name that is already spelled out — are returned unchanged.
func languageName(value string) string {
	tag, err := language.Parse(value)
	if err != nil {
		return value
	}
	if name := display.English.Tags().Name(tag); name != "" {
		return name
	}
	return value
}

func sendSSEJSON(stream *sse.Writer, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return stream.Send(event, string(payload))
}
