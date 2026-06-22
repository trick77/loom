package httpapi

import (
	"context"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

const projectDescriptionCompletedTurnThreshold = 2

// maybeAutoDescribeProject runs synchronously while the SSE stream is still
// open so the generated project can be emitted immediately. The helper is
// one-shot and best-effort, unlike the detached memory refreshes below the
// stream path, because a goroutine after handler return would have no live
// stream to write the project update to.
func (s *server) maybeAutoDescribeProject(ctx, persistCtx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, messages []chat.Message) {
	if thread.ProjectID == nil {
		return
	}
	if completedTurns(messages) < projectDescriptionCompletedTurnThreshold {
		return
	}
	project, found, err := s.thread.GetProject(persistCtx, user.ID, *thread.ProjectID)
	if err != nil {
		slog.Warn("load project for auto description failed", "project_id", *thread.ProjectID, "error", err)
		return
	}
	if !found || strings.TrimSpace(project.Description) != "" || project.AutoDescriptionGeneratedAt != nil {
		return
	}
	transcript := transcriptFromMessages(messages)
	if strings.TrimSpace(transcript) == "" {
		return
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: thread.ID, Purpose: "project_description", Round: 1}
	description, err := s.llm.GenerateProjectDescription(llm.WithInferenceMetadata(ctx, inference), project.Name, transcript)
	if err != nil {
		slog.Warn("generate project description failed", "project_id", project.ID, "error", err)
		return
	}
	if strings.TrimSpace(description) == "" {
		return
	}
	updated, changedDescription, err := s.thread.SetProjectDescriptionIfEmpty(persistCtx, user.ID, project.ID, description)
	if err != nil {
		slog.Warn("persist project description failed", "project_id", project.ID, "error", err)
		return
	}
	if !changedDescription {
		return
	}
	_ = sendSSEJSON(stream, "project", updated)
}

func completedTurns(messages []chat.Message) int {
	turns := 0
	waitingForAssistant := false
	for _, message := range messages {
		switch message.Role {
		case chat.RoleUser:
			waitingForAssistant = true
		case chat.RoleAssistant:
			if waitingForAssistant {
				turns++
				waitingForAssistant = false
			}
		}
	}
	return turns
}
