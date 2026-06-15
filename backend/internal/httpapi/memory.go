package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/trick77/lume/internal/auth"
	"github.com/trick77/lume/internal/chat"
	"github.com/trick77/lume/internal/llm"
)

// Memory tuning, shared by the project and user memories.
const (
	// memoryRefreshThreshold is how many new messages must accumulate (since the
	// last refresh) before the background auto-refresh runs — the "after a few
	// chats" gate.
	memoryRefreshThreshold = 4
	// memoryRebuildLimit caps how many recent messages a full rebuild reads, so
	// it never loads the entire history.
	memoryRebuildLimit = 200
	// memoryTranscriptLimit caps how many recent messages feed an incremental
	// refresh.
	memoryTranscriptLimit = 40
	// memoryBackgroundTimeout bounds a background refresh's LLM call.
	memoryBackgroundTimeout = 2 * time.Minute
)

// memoryScope describes one memory's storage and generation so the project and
// user memories share a single refresh/generate mechanism. Each scope closes
// over its user (and, for projects, its project), supplying scope-specific
// storage access, the generation header, and the system prompt.
type memoryScope struct {
	name         string // for logs, e.g. "project" / "user"
	purpose      string // inference metadata purpose
	header       string // generation header block (e.g. project name/description)
	systemPrompt string // llm system prompt selecting the memory's style

	get    func(ctx context.Context) (content string, sourceCount int, err error)
	upsert func(ctx context.Context, content string, sourceCount int) error
	count  func(ctx context.Context) (int, error)
	list   func(ctx context.Context, limit int) ([]chat.Message, error)
}

// refreshMemoryIfDue runs an incremental refresh when the gate is met. It is the
// synchronous core of the background job (split out so it is testable without a
// goroutine).
func (s *server) refreshMemoryIfDue(ctx context.Context, user auth.User, scope memoryScope) error {
	count, err := scope.count(ctx)
	if err != nil {
		return err
	}
	prior, sourceCount, err := scope.get(ctx)
	if err != nil {
		return err
	}
	if count-sourceCount < memoryRefreshThreshold {
		return nil
	}
	// Fold the prior memory with the most recent messages (bounded by
	// memoryTranscriptLimit).
	messages, err := scope.list(ctx, memoryTranscriptLimit)
	if err != nil {
		return err
	}
	return s.refreshMemory(ctx, user, scope, prior, messages, count)
}

// refreshMemory generates and stores an updated memory. When prior is non-empty
// it folds the given transcript into it (incremental); the caller passes the
// recent (bounded) messages for that.
func (s *server) refreshMemory(ctx context.Context, user auth.User, scope memoryScope, prior string, transcriptMessages []chat.Message, sourceCount int) error {
	transcript := transcriptFromMessages(transcriptMessages)
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: scope.purpose, Round: 1}
	content, err := s.llm.GenerateMemory(llm.WithInferenceMetadata(ctx, inference), scope.header, prior, transcript, scope.systemPrompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return scope.upsert(ctx, content, sourceCount)
}

// transcriptFromMessages renders messages as a plain "Role: content" transcript
// for memory generation. Only user/assistant turns are included.
func transcriptFromMessages(messages []chat.Message) string {
	var b strings.Builder
	for _, m := range messages {
		if m.Role != chat.RoleUser && m.Role != chat.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(roleLabel(m.Role))
		b.WriteString(": ")
		b.WriteString(content)
	}
	return b.String()
}

func roleLabel(role chat.Role) string {
	switch role {
	case chat.RoleAssistant:
		return "Assistant"
	default:
		return "User"
	}
}
