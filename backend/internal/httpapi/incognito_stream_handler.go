package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

// incognitoThreadID is the synthetic id used for the ephemeral incognito thread.
// It is never written to the store — it exists only to satisfy the inference
// metadata and the SSE payload shapes the client already understands.
const incognitoThreadID = "incognito"

// handleIncognitoStreamMessage answers a single ephemeral turn and persists
// NOTHING: no threads/messages/artifacts rows, no usage accounting, and no user
// or project memory — neither read nor written. The client owns the transcript
// and replays it as History on every turn (the server has nothing to reload).
//
// It is deliberately a stripped-down sibling of handleStreamMessage: no
// GetThread/ListMessages/AddMessage*, no title generation, no attachments, no
// RAG/knowledge, no memory refresh. Tools are disabled entirely (see
// runIncognitoAssistantTurn) so no tool can write to the DB or disk.
func (s *server) handleIncognitoStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	var body incognitoStreamRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSONError(w, http.StatusBadRequest, "message content is required")
		return
	}
	if len(body.Content) > chat.MaxMessageContentLength {
		writeJSONError(w, http.StatusBadRequest, "message content is too long")
		return
	}
	// The whole transcript is client-supplied every turn; bound it so a caller can't
	// force an unboundedly large prompt build. The model context window is the real
	// ceiling — this is a coarse guard well above any legitimate incognito session.
	if incognitoHistoryTooLarge(body.History) {
		writeJSONError(w, http.StatusBadRequest, "conversation is too long")
		return
	}

	// Empty classifier/user/project/knowledge/document contexts — incognito reads
	// no saved memory, matching its "not added to memory" promise on the read side
	// too. buildLLMHistory still supplies the base system prompt so the model
	// behaves normally.
	priorMessages := incognitoPriorMessages(body.History)
	userMessage := chat.Message{Role: chat.RoleUser, Content: body.Content}
	history := buildLLMHistory(user, "", "", "", "", "", priorMessages, userMessage)

	streamCtx, cancelStream := context.WithCancelCause(r.Context())
	defer cancelStream(nil)
	turnStart := time.Now()

	stream, err := sse.NewWriter(w)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "client_message", "sse writer init failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Heartbeat(streamCtx, streamHeartbeatInterval)()

	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: incognitoThreadID, Incognito: true}
	titles := newReasoningTitleTracker(s, stream, streamCtx, inference, userResponseLanguage(user))
	defer titles.wait()

	assistantResult, err := s.runIncognitoAssistantTurn(streamCtx, stream, titles, history, inference)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Info("incognito stream canceled",
				"content_bytes", len(assistantResult.Content),
				"reasoning_bytes", len(assistantResult.ReasoningContent))
			return
		}
		message := "stream failed"
		var userErr streamUserError
		switch {
		case errors.As(err, &userErr):
			message = userErr.message
		case errors.Is(err, llm.ErrStreamStalled):
			message = llm.ErrStreamStalled.Error()
			slog.Warn("incognito stream stalled", "reasoning_bytes", len(assistantResult.ReasoningContent))
		}
		_ = sendSSEJSON(stream, "error", map[string]string{"error": message})
		return
	}

	assistantContent := assistantResult.Content
	if strings.TrimSpace(assistantContent) == "" {
		slog.Warn("empty incognito assistant response",
			"content_bytes", len(assistantResult.Content),
			"reasoning_bytes", len(assistantResult.ReasoningContent))
		_ = sendSSEJSON(stream, "error", map[string]string{"error": "empty assistant response"})
		return
	}

	// Ensure background reasoning titles land before the trace/blocks are emitted,
	// mirroring the persisted path — but here the "message" is assembled in memory
	// and never stored.
	titles.wait()
	titles.mergeInto(assistantResult.ActivityTrace)
	titles.mergeIntoBlocks(assistantResult.Blocks)

	activityTraceJSON, err := json.Marshal(assistantResult.ActivityTrace)
	if err != nil {
		activityTraceJSON = []byte("[]")
	}
	contentBlocksJSON := []byte("[]")
	if len(assistantResult.Blocks) > 0 {
		if encoded, marshalErr := json.Marshal(assistantResult.Blocks); marshalErr == nil {
			contentBlocksJSON = encoded
		}
	}

	assistantMessage := chat.Message{
		ID:            "incognito-assistant",
		ThreadID:      incognitoThreadID,
		Role:          chat.RoleAssistant,
		Content:       assistantContent,
		ToolCalls:     json.RawMessage("[]"),
		Citations:     json.RawMessage("[]"),
		Artifacts:     json.RawMessage("[]"),
		Attachments:   json.RawMessage("[]"),
		ActivityTrace: activityTraceJSON,
		ContentBlocks: contentBlocksJSON,
		CreatedAt:     time.Now(),
	}
	applyMessageMetrics(&assistantMessage, messageMetricsFromTurn(assistantResult.StreamResult, llm.TokenUsage{}, time.Since(turnStart)))
	if err := sendSSEJSON(stream, "assistant_message", assistantMessage); err != nil {
		return
	}
	_ = stream.Send("done", "{}")
}

// maxIncognitoHistoryBytes caps the combined size of the client-supplied prior
// turns. It is intentionally generous — dozens of full-length messages — since the
// real limit is the model's context window; this only rejects pathological input.
const maxIncognitoHistoryBytes = 40 * chat.MaxMessageContentLength

func incognitoHistoryTooLarge(history []incognitoHistoryEntry) bool {
	total := 0
	for _, entry := range history {
		total += len(entry.Content)
		if total > maxIncognitoHistoryBytes {
			return true
		}
	}
	return false
}

// incognitoPriorMessages converts the client-supplied history into the store's
// Message shape for buildLLMHistory. Only user/assistant turns are kept; anything
// else (e.g. a stray tool role) is dropped.
func incognitoPriorMessages(history []incognitoHistoryEntry) []chat.Message {
	messages := make([]chat.Message, 0, len(history))
	for _, entry := range history {
		role := chat.Role(entry.Role)
		if role != chat.RoleUser && role != chat.RoleAssistant {
			continue
		}
		messages = append(messages, chat.Message{Role: role, Content: entry.Content})
	}
	return messages
}

// applyMessageMetrics copies the turn's per-message stats onto an in-memory
// message so the incognito assistant bubble shows the same model/token/duration
// footer as a persisted one, without ever touching the store.
func applyMessageMetrics(message *chat.Message, metrics chat.MessageTokenUsage) {
	message.ReasoningContent = metrics.ReasoningContent
	message.Model = metrics.Model
	message.ReasoningEffort = metrics.ReasoningEffort
	message.DurationMs = metrics.DurationMs
	message.PromptTokens = metrics.PromptTokens
	message.CompletionTokens = metrics.CompletionTokens
	message.TotalTokens = metrics.TotalTokens
	message.CachedTokens = metrics.CachedTokens
	message.ReasoningTokens = metrics.ReasoningTokens
	message.ContextTokens = metrics.ContextTokens
}
