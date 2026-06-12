package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/sse"
	"github.com/trick77/slopr/internal/usage"
)

const sloprSystemPrompt = "Write in flowing prose by default — full sentences grouped into paragraphs — no matter the topic or how long the answer is. Presenting information, explaining or describing something is prose, not a list. Do NOT use bullet points or numbered lists to break an answer into points. Use a list ONLY when the content is a true enumeration the user would naturally keep as a list: sequential steps to follow, distinct API parameters, or a literal checklist. When in doubt, use prose. For brainstorming, naming, or idea-generation requests without an explicit count, give at most 12 options and recommend one. Put code in fenced markdown blocks. When unsure, use available tools to find the answer before responding; if they turn up nothing, say you don't know rather than guessing. If you are about to say a topic is beyond your knowledge, too recent, or past your training cutoff, first use the available search and fetch tools to look it up; only say you don't know after those tools return nothing useful. For image or logo generation, editing, restyling, or variation requests, call the image generation tool before answering. Never claim that an image was generated unless an image artifact was actually created. For URLs, use the lightweight fetch tool first when the task is to read, summarize, quote, or extract page text. Use browser tools only when fetch cannot access useful content, the user asks for visual inspection, or the task requires interaction, navigation, screenshots, login/session behavior, or JavaScript-rendered state. Ignore the language of tool results and retrieved documents."

const imagePromptCompilerSystemPrompt = "The latest user request requires image generation or editing. Your only job is to call `generate_image` exactly once. Do not answer conversationally before the tool call. Do not refuse based on being text-based. Transform the user's request into a concise, visually rich prompt that preserves subject, setting, style, composition, mood, medium, text requirements, and constraints. Add only helpful visual details consistent with the request. Use `filename` when obvious. After the tool result, provide a brief final response that refers to the created artifact. Never claim an image was created unless the tool result confirms an artifact."

var (
	errStreamStopRequested = errors.New("stream stop requested")
	errStreamSuperseded    = errors.New("stream superseded by newer request")
)

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
	streamCtx, cancelStream := context.WithCancelCause(r.Context())
	defer cancelStream(nil)
	// Sum token usage across every model call this turn makes — answer turns, tool
	// rounds, and the background reasoning/thread-title helpers — so the persisted
	// per-message stats reflect the whole turn, not just the final answer call.
	// turnStart times the full turn wall-clock for the same reason.
	usageTotal := llm.NewUsageAccumulator()
	streamCtx = llm.WithUsageAccumulator(streamCtx, usageTotal)
	turnStart := time.Now()
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

	userContext := s.userContextForUser(r.Context(), user.ID)
	projectContext := s.projectContextForThread(r.Context(), user.ID, thread)
	knowledgeContext, knowledgeSources := s.knowledgeContextForThread(r.Context(), user.ID, thread, userMessage.Content)
	if len(knowledgeSources) > 0 {
		_ = sendSSEJSON(stream, "knowledge_sources", map[string]any{"sources": knowledgeSources})
	}
	history := buildLLMHistory(user, userContext, projectContext, knowledgeContext, priorMessages, userMessage)
	imageArtifactRequired := s.imageArtifactRequired(body.Content, priorMessages)
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID}
	// Background reasoning-title generation. The deferred wait is a safety net so
	// no title goroutine writes to the SSE stream after the handler returns on an
	// early error path.
	titles := newReasoningTitleTracker(s, stream, streamCtx, inference)
	defer titles.wait()
	assistantResult, err := s.runAssistantLoop(streamCtx, stream, titles, history, inference, user, thread, imageArtifactRequired)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancelSource, cancelReason := streamCancelDetails(streamCtx)
			slog.Info("message stream canceled",
				"thread_id", threadID,
				"cancel_source", cancelSource,
				"reason", cancelReason,
				"content_bytes", len(assistantResult.Content),
				"reasoning_bytes", len(assistantResult.ReasoningContent),
				"tool_calls", len(assistantResult.ToolCalls))
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
	if imageArtifactRequired && len(assistantResult.Artifacts) == 0 {
		message := "image generation was not completed"
		if strings.TrimSpace(assistantResult.ToolError) != "" {
			message = assistantResult.ToolError
		}
		slog.Warn("image request completed without image artifact",
			"thread_id", threadID,
			"content_bytes", len(assistantResult.Content),
			"tool_calls", len(assistantResult.ToolCalls),
			"tool_error", assistantResult.ToolError)
		_ = sendSSEJSON(stream, "error", map[string]string{"error": message})
		return
	}
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
	artifacts := assistantResult.Artifacts
	if artifacts == nil {
		artifacts = []artifactResponse{}
	}
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		return
	}
	// Ensure every background title has landed and been emitted before persisting
	// the trace; this also guarantees the title SSE events precede assistant_message.
	titles.wait()
	titles.mergeInto(assistantResult.ActivityTrace)
	activityTraceJSON, err := json.Marshal(assistantResult.ActivityTrace)
	if err != nil {
		slog.Warn("marshal activity trace failed", "thread_id", threadID, "error", err)
		activityTraceJSON = []byte("[]")
	}
	// Persist the RAG citations so the answer's sources survive a reload.
	citationsJSON := json.RawMessage("[]")
	if len(knowledgeSources) > 0 {
		if encoded, marshalErr := json.Marshal(knowledgeSources); marshalErr == nil {
			citationsJSON = encoded
		}
	}
	assistantMessage, err := s.chat.AddMessageWithCitations(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageMetricsFromTurn(assistantResult.StreamResult, usageTotal.Total(), time.Since(turnStart)), artifactsJSON, activityTraceJSON, citationsJSON)
	if err != nil {
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "persist assistant message failed"})
		return
	}
	if err := sendSSEJSON(stream, "assistant_message", assistantMessage); err != nil {
		return
	}

	// Add this turn's token burn to the user's lifetime totals. usageTotal was
	// summed across the answer, tool rounds, and the title/reasoning helpers, and
	// is read here after titles.wait() above so helper-call tokens are included —
	// matching the per-message stats persisted just above.
	turnUsage := usageTotal.Total()
	if turnUsage.Present() {
		s.recordUsage("tokens", func() error {
			return s.usage.AddTokens(persistCtx, user.ID, usage.TokenDelta{
				PromptTokens:     turnUsage.PromptTokens,
				CompletionTokens: turnUsage.CompletionTokens,
				CachedTokens:     turnUsage.PromptTokensDetails.CachedTokens,
				ReasoningTokens:  turnUsage.CompletionTokenDetails.ReasoningTokens,
				TotalTokens:      turnUsage.TotalTokens,
			})
		})
	}

	turnMessages := make([]chat.Message, 0, len(priorMessages)+2)
	turnMessages = append(turnMessages, priorMessages...)
	turnMessages = append(turnMessages, userMessage, assistantMessage)
	s.maybeAutoDescribeProject(r.Context(), persistCtx, stream, user, thread, turnMessages)

	// Best-effort, gated background refresh of the project's shared memory so
	// sibling chats stay aware of this turn. Detaches from the request context so
	// it survives the handler returning.
	s.maybeRefreshProjectMemoryAsync(r.Context(), user, thread)
	// Best-effort, gated background refresh of the user's personal memory so the
	// assistant stays personalized across all chats. Applies to every chat
	// (project-bound or not). Detaches from the request context too.
	s.maybeRefreshUserMemoryAsync(r.Context(), user)

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
	s.activeStreams.stop(user.ID, threadID, errStreamStopRequested)
	w.WriteHeader(http.StatusNoContent)
}

type streamUserError struct {
	message string
}

func (e streamUserError) Error() string {
	return e.message
}

func sendSSEJSON(stream *sse.Writer, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return stream.Send(event, string(payload))
}

func streamCancelDetails(ctx context.Context) (string, string) {
	cause := context.Cause(ctx)
	if cause == nil {
		return "", ""
	}
	source := "unknown"
	switch {
	case errors.Is(cause, errStreamStopRequested):
		source = "stop_endpoint"
	case errors.Is(cause, errStreamSuperseded):
		source = "superseded_stream"
	case errors.Is(cause, context.Canceled):
		source = "request_context"
	case errors.Is(cause, context.DeadlineExceeded):
		source = "deadline"
	}
	return source, cause.Error()
}
