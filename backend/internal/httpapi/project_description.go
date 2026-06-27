package httpapi

import (
	"context"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// maybeAutoDescribeProject one-shot fills an empty project description from the
// given transcript. It rides the project-memory refresh (see refreshMemory), so
// it runs whenever the project's memory is (re)calculated — asynchronously, off
// the request hot path — rather than on its own per-turn trigger. Best-effort:
// errors are logged, never surfaced. The empty/one-shot guard here mirrors the
// atomic guard in SetProjectDescriptionIfEmpty (which re-checks under the write,
// so a stale in-memory project can never overwrite a description set meanwhile).
func (s *server) maybeAutoDescribeProject(ctx context.Context, user auth.User, project chat.Project, transcript string) {
	if strings.TrimSpace(project.Description) != "" || project.AutoDescriptionGeneratedAt != nil {
		return
	}
	if strings.TrimSpace(transcript) == "" {
		return
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "project_description", Round: 1}
	description, err := s.llm.GenerateProjectDescription(llm.WithInferenceMetadata(ctx, inference), project.Name, transcript)
	if err != nil {
		slog.Warn("generate project description failed", "project_id", project.ID, "error", err)
		return
	}
	if strings.TrimSpace(description) == "" {
		return
	}
	if _, _, err := s.thread.SetProjectDescriptionIfEmpty(ctx, user.ID, project.ID, description); err != nil {
		slog.Warn("persist project description failed", "project_id", project.ID, "error", err)
	}
}
