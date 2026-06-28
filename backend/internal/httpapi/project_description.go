package httpapi

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/llm"
)

// maybeRefreshProjectDescriptionAsync refreshes a project's auto-generated
// description in the background. The description is a one-sentence big-picture summary
// of the project's thread titles, so it is refreshed when a new thread is titled (the
// caller fires this from the thread-title finalization path) and as a backstop from
// the memory sweep. It detaches from the request context so it survives the handler
// returning, and is best-effort (errors are logged, never surfaced). The actual work
// is gated/debounced in refreshProjectDescriptionIfDue, so a no-op call is cheap.
func (s *server) maybeRefreshProjectDescriptionAsync(parent context.Context, user auth.User, projectID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), memoryBackgroundTimeout)
		defer cancel()
		if err := s.refreshProjectDescriptionIfDue(ctx, user, projectID); err != nil {
			slog.Warn("background project description refresh failed", "project_id", projectID, "error", err)
		}
	}()
}

// refreshProjectDescriptionIfDue regenerates a project's auto-description from its
// thread titles when the gate is met, mirroring the project-memory refresh gate:
//
//   - skip if the user hand-edited the description (DescriptionUserEdited) — manual
//     descriptions are locked and never overwritten;
//   - skip when the titled-thread count is unchanged since the last generation
//     (DescriptionSourceThreadCount) — nothing new to summarize;
//   - debounce: skip when the description was regenerated within memoryProjectDebounce
//     (a never-generated description has no marker and is always due).
//
// The count gate (rather than firing once on creation) is what guarantees "big picture
// always": a thread titled inside a debounce window is caught on the next trigger or
// sweep because the count still differs, instead of being stranded.
func (s *server) refreshProjectDescriptionIfDue(ctx context.Context, user auth.User, projectID string) error {
	project, err := s.findProject(ctx, user.ID, projectID)
	if err != nil || project == nil {
		return err
	}
	if project.DescriptionUserEdited {
		return nil
	}
	titles, err := s.thread.ListProjectThreadTitles(ctx, user.ID, projectID)
	if err != nil {
		return err
	}
	if len(titles) == 0 {
		return nil
	}
	if len(titles) == project.DescriptionSourceThreadCount {
		return nil
	}
	if project.AutoDescriptionGeneratedAt != nil && time.Since(*project.AutoDescriptionGeneratedAt) < memoryProjectDebounce {
		return nil
	}
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "project_description", Round: 1}
	description, err := s.llm.GenerateProjectDescription(llm.WithInferenceMetadata(ctx, inference), project.Name, titles)
	if err != nil {
		slog.Warn("generate project description failed", "project_id", projectID, "error", err)
		return nil
	}
	if strings.TrimSpace(description) == "" {
		return nil
	}
	// SetAutoProjectDescription re-checks the user-edited lock under the write, so a
	// description the user hand-edited between the load above and here is never
	// clobbered. The current titled-thread count is recorded as the new gate baseline.
	if _, _, err := s.thread.SetAutoProjectDescription(ctx, user.ID, projectID, description, len(titles)); err != nil {
		slog.Warn("persist project description failed", "project_id", projectID, "error", err)
	}
	return nil
}
