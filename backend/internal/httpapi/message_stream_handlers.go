package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/docgen"
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
	streamCtx, cancelStream := context.WithCancel(r.Context())
	defer cancelStream()
	unregisterStream := s.activeStreams.register(user.ID, threadID, cancelStream)
	defer unregisterStream()

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
	mcpStatusCh := s.startMCPStatus(streamCtx)

	if thread.Title == chat.DefaultThreadTitle {
		_ = s.generateAndSendThreadTitle(streamCtx, context.WithoutCancel(r.Context()), stream, user, threadID, userMessage.Content, "")
	}

	history := buildLLMHistory(user, priorMessages, userMessage)
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID}
	assistantResult, err := s.runAssistantLoop(streamCtx, stream, history, inference, user, thread)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
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
	artifactsJSON, err := json.Marshal(assistantResult.Artifacts)
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		return
	}
	assistantMessage, err := s.chat.AddMessageWithArtifacts(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageMetricsFromResult(assistantResult.StreamResult), artifactsJSON)
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

func (s *server) handleStopStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	_, found, err := s.chat.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	s.activeStreams.stop(user.ID, threadID)
	w.WriteHeader(http.StatusNoContent)
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

type assistantLoopResult struct {
	llm.StreamResult
	Artifacts []artifactResponse
}

func (s *server) runAssistantLoop(ctx context.Context, stream *sse.Writer, history []llm.Message, inference llm.InferenceMetadata, user auth.User, thread chat.Thread) (assistantLoopResult, error) {
	tools := s.availableTools()
	if len(tools) == 0 {
		result, err := s.streamAssistantTurn(ctx, stream, history, inferenceWithPurpose(inference, "chat", 1), nil)
		if persistCanceledPartial(result, err) {
			return assistantLoopResult{StreamResult: result}, nil
		}
		return assistantLoopResult{StreamResult: result}, err
	}

	toolRan := false
	var artifacts []artifactResponse
	for round := 1; round <= maxToolRounds; round++ {
		result, err := s.streamAssistantTurn(ctx, stream, history, inferenceWithPurpose(inference, "chat_tool_round", round), tools)
		if err != nil {
			if persistCanceledPartial(result, err) {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts}, nil
			}
			return assistantLoopResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			// A normal textual answer ends the loop. But if the model stops
			// after running tools without producing any text, fall through to a
			// forced, tool-free final answer instead of returning an empty (and
			// therefore discarded) response.
			if strings.TrimSpace(result.Content) != "" || !toolRan {
				return assistantLoopResult{StreamResult: result, Artifacts: artifacts}, nil
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
	result, err := s.streamAssistantTurn(ctx, stream, finalHistory, inferenceWithPurpose(inference, "chat_final", maxToolRounds+1), nil)
	if persistCanceledPartial(result, err) {
		return assistantLoopResult{StreamResult: result, Artifacts: artifacts}, nil
	}
	return assistantLoopResult{StreamResult: result, Artifacts: artifacts}, err
}

func persistCanceledPartial(result llm.StreamResult, err error) bool {
	return errors.Is(err, context.Canceled) && strings.TrimSpace(result.Content) != ""
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

func (s *server) availableTools() []llm.Tool {
	tools := []llm.Tool(nil)
	names := map[string]string{}
	if s.artifacts != nil && strings.TrimSpace(s.usersDir) != "" {
		for _, gen := range s.docTools {
			schema := gen.Schema()
			names[schema.Name] = "built_in"
			tools = append(tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        schema.Name,
					Description: schema.Description,
					Parameters:  schema.Parameters,
				},
			})
		}
	}
	if s.mcp != nil {
		for _, tool := range s.mcp.Tools() {
			if owner, exists := names[tool.Function.Name]; exists {
				slog.Warn("skipping duplicate MCP tool name", "tool", tool.Function.Name, "existing", owner)
				continue
			}
			names[tool.Function.Name] = "mcp"
			tools = append(tools, tool)
		}
	}
	return tools
}

func (s *server) executeBuiltInTool(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, call llm.ToolCall) (string, *artifactResponse, bool) {
	generator := s.docGenerator(call.Function.Name)
	if generator == nil {
		return "", nil, false
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return capToolOutput("tool failed: invalid arguments: " + err.Error()), nil, true
	}
	filename, _ := args["filename"].(string)
	var buffer bytes.Buffer
	meta, err := generator.Generate(docgen.GenerateRequest{
		Format:   generator.ToolName(),
		Filename: filename,
		Payload:  args,
	}, &buffer)
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil, true
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return "tool failed: generated file is too large", nil, true
	}
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
	})
	if err != nil {
		return capToolOutput("tool failed: " + err.Error()), nil, true
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: write artifact: " + err.Error()), nil, true
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: close artifact: " + err.Error()), nil, true
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        out.MIMEType,
		SizeBytes:       int64(buffer.Len()),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return capToolOutput("tool failed: persist artifact: " + err.Error()), nil, true
	}
	response := artifactResponse{
		ID:              created.ID,
		DisplayFilename: created.DisplayFilename,
		MIMEType:        created.MIMEType,
		SizeBytes:       created.SizeBytes,
		ProjectID:       created.ProjectID,
		DownloadURL:     created.DownloadURL,
	}
	_ = sendSSEJSON(stream, "artifact", response)
	return fmt.Sprintf("created artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), &response, true
}

func (s *server) docGenerator(name string) docgen.Generator {
	for _, candidate := range s.docTools {
		if candidate.ToolName() == name {
			return candidate
		}
	}
	return nil
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
