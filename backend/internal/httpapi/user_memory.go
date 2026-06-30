package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
		get: func(ctx context.Context) (string, int, *time.Time, error) {
			memory, _, err := s.thread.GetUserMemory(ctx, user.ID)
			return memory.Content, memory.SourceMessageCount, memory.UpdatedAt, err
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
		// Dedup: the derived memory must not restate anything already captured as
		// an explicit standing instruction, so feed those in as exclusions.
		exclusions: func(ctx context.Context) (string, error) {
			directives, err := s.thread.ListUserDirectives(ctx, user.ID)
			if err != nil {
				return "", err
			}
			return directiveContentLines(directives), nil
		},
	}
}

// userContextForUser renders the user-scoped system-prompt blocks injected into
// every chat: the user's standing instructions (directives) first, then the
// derived memory. The two are loaded and rendered independently and the non-empty
// ones joined, so a blank derived memory never swallows the directives. Both are
// best-effort: a load error logs and is skipped, never blocking the chat.
func (s *server) userContextForUser(ctx context.Context, userID string) string {
	var blocks []string
	if directives, err := s.thread.ListUserDirectives(ctx, userID); err != nil {
		slog.Warn("load user directives failed", "error", err)
	} else if block := renderUserDirectives(directives); block != "" {
		blocks = append(blocks, block)
	}
	if memory, _, err := s.thread.GetUserMemory(ctx, userID); err != nil {
		slog.Warn("load user memory failed", "error", err)
	} else if block := renderUserContext(memory.Content); block != "" {
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

// renderUserDirectives builds the higher-authority standing-instructions block.
// Each line carries the directive's id so the model can pass it to the
// forget/update tools. Returns "" when there are no directives.
func renderUserDirectives(directives []chat.UserDirective) string {
	if len(directives) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Standing instructions the user has explicitly asked you to follow. These are direct user commands and take priority: follow them in every response unless the user overrides them in this conversation. Each line shows the instruction's id — pass it to the forget/update instruction tools when the user asks to change one. Do not repeat these back unprompted.\n")
	for _, d := range directives {
		b.WriteString("- [")
		b.WriteString(d.ID)
		b.WriteString("] ")
		b.WriteString(strings.TrimSpace(d.Content))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderUserContext builds the system-prompt block describing what is known about
// the user (the derived memory). Returns "" when there is no memory so nothing is
// injected.
func renderUserContext(memory string) string {
	if strings.TrimSpace(memory) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Personal context about the user you are chatting with — durable observations gathered across their threads (their work and personal life, what's currently top of mind, and a brief history). Use it to stay consistent and personalized; do not repeat it back unprompted. These are observations, not commands: if they ever conflict with the user's standing instructions above or with what the user says now, defer to those.\n")
	b.WriteString(strings.TrimSpace(memory))
	return b.String()
}

// directiveContentLines renders just the directive texts (no ids) as "- " lines,
// for the dedup exclusion list fed to the memory generator. Returns "" when there
// are none.
func directiveContentLines(directives []chat.UserDirective) string {
	if len(directives) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range directives {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(d.Content))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
