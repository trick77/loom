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

// maybeBackfillProjectDescription fills an empty project description independently
// of the memory-refresh activity gate. maybeAutoDescribeProject rides
// refreshMemory, which the activity gate (count <= source_message_count) skips for
// a project that has no new messages since its last refresh — so a project that is
// missing its description (e.g. one whose description was later cleared, re-arming
// the marker) would otherwise never regenerate it without fresh activity. The
// background MemoryWorker calls this every sweep; the empty/one-shot guard plus the
// atomic SetProjectDescriptionIfEmpty make it a cheap no-op once a description
// exists. It re-reads the project so a description just set by the gated refresh in
// the same sweep is seen, avoiding a redundant generation.
//
// Retry policy: if generation returns empty (e.g. the model produced no salvageable
// text), no description is stored and the marker is left unset, so the next sweep
// retries. This is deliberate — a missing description should self-heal — and the cost
// is bounded by the sweep cadence (one attempt per project per sweep), not per turn.
// GenerateProjectDescription salvages truncated replies, so a genuinely empty result
// means a real upstream outage, where retrying on the next sweep is the right behavior.
func (s *server) maybeBackfillProjectDescription(ctx context.Context, user auth.User, project chat.Project) {
	fresh, ok, err := s.thread.GetProject(ctx, user.ID, project.ID)
	if err != nil {
		slog.Warn("backfill project description: load project failed", "project_id", project.ID, "error", err)
		return
	}
	if !ok || strings.TrimSpace(fresh.Description) != "" || fresh.AutoDescriptionGeneratedAt != nil {
		return
	}
	messages, err := s.thread.ListProjectMessages(ctx, user.ID, project.ID, memoryRebuildLimit)
	if err != nil {
		slog.Warn("backfill project description: list messages failed", "project_id", project.ID, "error", err)
		return
	}
	s.maybeAutoDescribeProject(ctx, user, fresh, transcriptFromMessages(messages))
}
