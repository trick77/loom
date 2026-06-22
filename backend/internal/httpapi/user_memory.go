package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// userMemoryScope wires the user memory into the shared memory mechanism. Unlike
// the project scope it has no header block — the system prompt fully describes
// the task — and it draws from every thread the user owns.
func (s *server) userMemoryScope(user auth.User) memoryScope {
	return memoryScope{
		name:         "user",
		purpose:      "user_memory",
		header:       "",
		systemPrompt: llm.UserMemorySystemPrompt,
		get: func(ctx context.Context) (string, int, error) {
			memory, _, err := s.thread.GetUserMemory(ctx, user.ID)
			return memory.Content, memory.SourceMessageCount, err
		},
		upsert: func(ctx context.Context, content string, sourceCount int) error {
			_, err := s.thread.UpsertUserMemory(ctx, user.ID, content, sourceCount)
			return err
		},
		count: func(ctx context.Context) (int, error) {
			return s.thread.CountUserMessages(ctx, user.ID)
		},
		list: func(ctx context.Context, limit int) ([]chat.Message, error) {
			return s.thread.ListUserMessages(ctx, user.ID, limit)
		},
	}
}

// userContextForUser loads the user's memory and renders the system-prompt block
// injected into every chat. Returns "" when there is no memory yet or on any
// error (user context is best-effort and never blocks a chat).
func (s *server) userContextForUser(ctx context.Context, userID string) string {
	memory, _, err := s.thread.GetUserMemory(ctx, userID)
	if err != nil {
		slog.Warn("load user memory failed", "error", err)
		return ""
	}
	return renderUserContext(memory.Content)
}

// renderUserContext builds the system-prompt block describing what is known
// about the user. Returns "" when there is no memory so nothing is injected.
func renderUserContext(memory string) string {
	if strings.TrimSpace(memory) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Personal context about the user you are chatting with (durable facts they shared across threads). Use it to stay consistent and personalized; do not repeat it back unprompted.\n")
	b.WriteString(strings.TrimSpace(memory))
	return b.String()
}

// maybeRefreshUserMemoryAsync incrementally refreshes the user's memory in the
// background after an assistant turn, gated so it only runs once enough new
// messages have accumulated. Unlike the project variant it has no project gate —
// it applies to every chat. It detaches from the request context so it survives
// the handler returning, and is best-effort (errors are logged, never surfaced).
func (s *server) maybeRefreshUserMemoryAsync(parent context.Context, user auth.User) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), memoryBackgroundTimeout)
		defer cancel()
		if err := s.refreshMemoryIfDue(ctx, user, s.userMemoryScope(user)); err != nil {
			slog.Warn("background user memory refresh failed", "user_id", user.ID, "error", err)
		}
	}()
}

func (s *server) handleGetUserMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	memory, _, err := s.thread.GetUserMemory(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "get user memory failed")
		return
	}
	writeJSON(w, memory)
}

// handleRefreshUserMemory forces a full rebuild from the user's most recent
// messages across all threads (bounded by memoryRebuildLimit).
func (s *server) handleRefreshUserMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	count, err := s.thread.CountUserMessages(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "refresh user memory failed")
		return
	}
	messages, err := s.thread.ListUserMessages(r.Context(), user.ID, memoryRebuildLimit)
	if err != nil {
		serverError(w, r, err, "refresh user memory failed")
		return
	}
	// Full rebuild: ignore prior memory and re-summarize from scratch.
	if err := s.refreshMemory(r.Context(), user, s.userMemoryScope(user), "", messages, count); err != nil {
		writeJSONError(w, http.StatusBadGateway, "refresh user memory failed")
		return
	}
	memory, _, err := s.thread.GetUserMemory(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "get user memory failed")
		return
	}
	writeJSON(w, memory)
}
