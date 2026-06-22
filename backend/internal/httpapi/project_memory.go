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

// projectMemoryScope wires the project memory into the shared memory mechanism.
func (s *server) projectMemoryScope(user auth.User, project chat.Project) memoryScope {
	return memoryScope{
		name:         "project",
		purpose:      "project_memory",
		header:       projectMemoryHeader(project),
		systemPrompt: llm.ProjectMemorySystemPrompt,
		get: func(ctx context.Context) (string, int, error) {
			memory, _, err := s.thread.GetProjectMemory(ctx, user.ID, project.ID)
			return memory.Content, memory.SourceMessageCount, err
		},
		upsert: func(ctx context.Context, content string, sourceCount int) error {
			_, err := s.thread.UpsertProjectMemory(ctx, user.ID, project.ID, content, sourceCount)
			return err
		},
		count: func(ctx context.Context) (int, error) {
			return s.thread.CountProjectMessages(ctx, user.ID, project.ID)
		},
		list: func(ctx context.Context, limit int) ([]chat.Message, error) {
			return s.thread.ListProjectMessages(ctx, user.ID, project.ID, limit)
		},
	}
}

// projectMemoryHeader builds the generation header block describing the project.
func projectMemoryHeader(project chat.Project) string {
	var b strings.Builder
	b.WriteString("Project name:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(project.Name))
	b.WriteString("\n\"\"\"\n")
	if strings.TrimSpace(project.Description) != "" {
		b.WriteString("\nProject description:\n\"\"\"\n")
		b.WriteString(strings.TrimSpace(project.Description))
		b.WriteString("\n\"\"\"\n")
	}
	return b.String()
}

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
	memory, _, err := s.thread.GetProjectMemory(ctx, userID, *thread.ProjectID)
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
	b.WriteString("This thread belongs to a project. Use the project context below to stay consistent with other threads in the same project.\n")
	b.WriteString("Project name: ")
	b.WriteString(strings.TrimSpace(project.Name))
	if strings.TrimSpace(project.Description) != "" {
		b.WriteString("\nProject description: ")
		b.WriteString(strings.TrimSpace(project.Description))
	}
	if strings.TrimSpace(memory) != "" {
		b.WriteString("\nProject memory (key facts and decisions from other threads):\n")
		b.WriteString(strings.TrimSpace(memory))
	}
	return b.String()
}

// maybeRefreshProjectMemoryAsync incrementally refreshes a project's memory in
// the background after an assistant turn, gated so it only runs once enough new
// messages have accumulated. It detaches from the request context so it survives
// the handler returning, and is best-effort (errors are logged, never surfaced).
func (s *server) maybeRefreshProjectMemoryAsync(parent context.Context, user auth.User, thread chat.Thread) {
	if thread.ProjectID == nil {
		return
	}
	projectID := *thread.ProjectID
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), memoryBackgroundTimeout)
		defer cancel()
		if err := s.refreshProjectMemoryIfDue(ctx, user, projectID); err != nil {
			slog.Warn("background project memory refresh failed", "project_id", projectID, "error", err)
		}
	}()
}

// refreshProjectMemoryIfDue runs an incremental refresh when the gate is met.
func (s *server) refreshProjectMemoryIfDue(ctx context.Context, user auth.User, projectID string) error {
	project, err := s.findProject(ctx, user.ID, projectID)
	if err != nil || project == nil {
		return err
	}
	return s.refreshMemoryIfDue(ctx, user, s.projectMemoryScope(user, *project))
}

// refreshProjectMemory generates and stores an updated memory from the given
// (bounded) messages. When prior is non-empty it folds the transcript into it.
func (s *server) refreshProjectMemory(ctx context.Context, user auth.User, projectID, prior string, transcriptMessages []chat.Message, sourceCount int) error {
	project, err := s.findProject(ctx, user.ID, projectID)
	if err != nil || project == nil {
		return err
	}
	return s.refreshMemory(ctx, user, s.projectMemoryScope(user, *project), prior, transcriptMessages, sourceCount)
}

func (s *server) findProject(ctx context.Context, userID, projectID string) (*chat.Project, error) {
	project, found, err := s.thread.GetProject(ctx, userID, projectID)
	if err != nil || !found {
		return nil, err
	}
	return &project, nil
}

func (s *server) handleGetProjectMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	projectID := r.PathValue("projectID")
	project, err := s.findProject(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "get project memory failed")
		return
	}
	if project == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	memory, _, err := s.thread.GetProjectMemory(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "get project memory failed")
		return
	}
	writeJSON(w, memory)
}

// handleRefreshProjectMemory forces a full rebuild from the most recent messages
// across all of the project's threads (bounded by memoryRebuildLimit).
func (s *server) handleRefreshProjectMemory(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	if s.llm == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "llm is not configured")
		return
	}
	projectID := r.PathValue("projectID")
	project, err := s.findProject(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "refresh project memory failed")
		return
	}
	if project == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	count, err := s.thread.CountProjectMessages(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "refresh project memory failed")
		return
	}
	messages, err := s.thread.ListProjectMessages(r.Context(), user.ID, projectID, memoryRebuildLimit)
	if err != nil {
		serverError(w, r, err, "refresh project memory failed")
		return
	}
	// Full rebuild: ignore prior memory and re-summarize from scratch.
	if err := s.refreshProjectMemory(r.Context(), user, projectID, "", messages, count); err != nil {
		writeJSONError(w, http.StatusBadGateway, "refresh project memory failed")
		return
	}
	memory, _, err := s.thread.GetProjectMemory(r.Context(), user.ID, projectID)
	if err != nil {
		serverError(w, r, err, "get project memory failed")
		return
	}
	writeJSON(w, memory)
}
