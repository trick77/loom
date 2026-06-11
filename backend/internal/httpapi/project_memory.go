package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/llm"
)

// Project-memory tuning.
const (
	// projectMemoryRefreshThreshold is how many new project messages must
	// accumulate (since the last refresh) before the background auto-refresh
	// runs — the "after a few chats" gate.
	projectMemoryRefreshThreshold = 4
	// projectMemoryRebuildLimit caps how many recent project messages a full
	// rebuild reads, so it never loads the entire project history.
	projectMemoryRebuildLimit = 200
	// projectMemoryTranscriptLimit caps how many messages of the current thread
	// feed an incremental refresh.
	projectMemoryTranscriptLimit = 40
	// projectMemoryBackgroundTimeout bounds a background refresh's LLM call.
	projectMemoryBackgroundTimeout = 2 * time.Minute
)

// projectContextForThread loads the project + memory for a thread and renders
// the system-prompt context block. Returns "" when the thread has no project or
// on any error (project context is best-effort and never blocks a chat).
func (s *server) projectContextForThread(ctx context.Context, userID string, thread chat.Thread) string {
	if thread.ProjectID == nil {
		return ""
	}
	project, err := s.findProject(ctx, userID, *thread.ProjectID)
	if err != nil {
		slog.Warn("load project for context failed", "error", err)
		return ""
	}
	if project == nil {
		return ""
	}
	memory, _, err := s.chat.GetProjectMemory(ctx, userID, *thread.ProjectID)
	if err != nil {
		slog.Warn("load project memory failed", "error", err)
		memory = chat.ProjectMemory{}
	}
	return renderProjectContext(*project, memory.Content)
}

// renderProjectContext builds the system-prompt block describing the project
// and its shared memory.
func renderProjectContext(project chat.Project, memory string) string {
	var b strings.Builder
	b.WriteString("This chat belongs to a project. Use the project context below to stay consistent with other chats in the same project.\n")
	b.WriteString("Project name: ")
	b.WriteString(strings.TrimSpace(project.Name))
	if strings.TrimSpace(project.Description) != "" {
		b.WriteString("\nProject description: ")
		b.WriteString(strings.TrimSpace(project.Description))
	}
	if strings.TrimSpace(memory) != "" {
		b.WriteString("\nProject memory (key facts and decisions from other chats):\n")
		b.WriteString(strings.TrimSpace(memory))
	}
	return b.String()
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

// maybeRefreshProjectMemoryAsync incrementally refreshes a project's memory in
// the background after an assistant turn, gated so it only runs once enough new
// messages have accumulated. It detaches from the request context so it survives
// the handler returning, and is best-effort (errors are logged, never surfaced).
func (s *server) maybeRefreshProjectMemoryAsync(parent context.Context, user auth.User, thread chat.Thread, threadMessages []chat.Message) {
	if thread.ProjectID == nil {
		return
	}
	projectID := *thread.ProjectID
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), projectMemoryBackgroundTimeout)
		defer cancel()

		count, err := s.chat.CountProjectMessages(ctx, user.ID, projectID)
		if err != nil {
			slog.Warn("count project messages failed", "project_id", projectID, "error", err)
			return
		}
		memory, _, err := s.chat.GetProjectMemory(ctx, user.ID, projectID)
		if err != nil {
			slog.Warn("get project memory failed", "project_id", projectID, "error", err)
			return
		}
		if count-memory.SourceMessageCount < projectMemoryRefreshThreshold {
			return
		}
		if err := s.refreshProjectMemory(ctx, user, projectID, memory.Content, threadMessages, count); err != nil {
			slog.Warn("refresh project memory failed", "project_id", projectID, "error", err)
		}
	}()
}

// refreshProjectMemory generates and stores an updated memory. When priorMemory
// is non-empty it folds the given transcript into it (incremental); the caller
// passes the recent thread messages for that. The project's name/description
// come from the loaded project.
func (s *server) refreshProjectMemory(ctx context.Context, user auth.User, projectID, priorMemory string, transcriptMessages []chat.Message, sourceCount int) error {
	project, err := s.findProject(ctx, user.ID, projectID)
	if err != nil || project == nil {
		return err
	}
	transcript := transcriptFromMessages(transcriptMessages)
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "project_memory", Round: 1}
	content, err := s.llm.GenerateProjectMemory(llm.WithInferenceMetadata(ctx, inference), project.Name, project.Description, priorMemory, transcript)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	_, err = s.chat.UpsertProjectMemory(ctx, user.ID, projectID, content, sourceCount)
	return err
}

func (s *server) findProject(ctx context.Context, userID, projectID string) (*chat.Project, error) {
	projects, err := s.chat.ListProjects(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		if projects[i].ID == projectID {
			return &projects[i], nil
		}
	}
	return nil, nil
}

func (s *server) handleGetProjectMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	projectID := r.PathValue("projectID")
	project, err := s.findProject(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get project memory failed")
		return
	}
	if project == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	memory, _, err := s.chat.GetProjectMemory(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get project memory failed")
		return
	}
	writeJSON(w, memory)
}

// handleRefreshProjectMemory forces a full rebuild from the most recent messages
// across all of the project's threads (bounded by projectMemoryRebuildLimit).
func (s *server) handleRefreshProjectMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireChat(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	projectID := r.PathValue("projectID")
	project, err := s.findProject(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "refresh project memory failed")
		return
	}
	if project == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	count, err := s.chat.CountProjectMessages(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "refresh project memory failed")
		return
	}
	messages, err := s.chat.ListProjectMessages(r.Context(), user.ID, projectID, projectMemoryRebuildLimit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "refresh project memory failed")
		return
	}
	// Full rebuild: ignore prior memory and re-summarize from scratch.
	if err := s.refreshProjectMemory(r.Context(), user, projectID, "", messages, count); err != nil {
		writeJSONError(w, http.StatusBadGateway, "refresh project memory failed")
		return
	}
	memory, _, err := s.chat.GetProjectMemory(r.Context(), user.ID, projectID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "get project memory failed")
		return
	}
	writeJSON(w, memory)
}
