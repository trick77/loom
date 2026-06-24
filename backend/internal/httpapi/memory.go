package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
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

// editMemory applies a user's natural-language instruction to the memory in
// place — adding, modifying, or removing facts as asked — and stores the result.
// It preserves the current source-message count so the background-refresh gate is
// undisturbed, and allows an empty result (the user emptied the memory).
func (s *server) editMemory(ctx context.Context, user auth.User, scope memoryScope, instruction string) error {
	current, sourceCount, err := scope.get(ctx)
	if err != nil {
		return err
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: scope.purpose, Round: 1}
	// scope.systemPrompt is reused here only to keep the OUTPUT format/style
	// consistent with the auto-generated memory (user = flat fact list, project =
	// markdown). ApplyMemoryEdit's own user message supplies the authoritative
	// "apply only this instruction, leave the rest unchanged" framing that
	// overrides the prompt's summarize-from-conversation wording.
	edited, err := s.llm.ApplyMemoryEdit(llm.WithInferenceMetadata(ctx, inference), scope.header, current, instruction, scope.systemPrompt)
	if err != nil {
		return err
	}
	return scope.upsert(ctx, strings.TrimSpace(edited), sourceCount)
}

// decodeMemoryInstruction reads the {"instruction": "..."} body shared by the
// user and project memory edit endpoints, writing a 400 and returning false when
// the body is malformed or the instruction is blank.
func decodeMemoryInstruction(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Instruction string `json:"instruction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	instruction := strings.TrimSpace(body.Instruction)
	if instruction == "" {
		writeJSONError(w, http.StatusBadRequest, "instruction is required")
		return "", false
	}
	return instruction, true
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
