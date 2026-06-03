package httpapi

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/trick77/spark/internal/artifact"
)

func (s *server) artifactsForThreadCleanup(ctx context.Context, userID, threadID string) ([]artifact.Artifact, error) {
	if s.artifacts == nil || strings.TrimSpace(s.usersDir) == "" {
		return nil, nil
	}
	return s.artifacts.ListForThread(ctx, userID, threadID)
}

func (s *server) artifactsForProjectCleanup(ctx context.Context, userID, projectID string) ([]artifact.Artifact, error) {
	if s.artifacts == nil || strings.TrimSpace(s.usersDir) == "" {
		return nil, nil
	}
	return s.artifacts.ListForProject(ctx, userID, projectID)
}

func (s *server) cleanupArtifactFiles(userID string, artifacts []artifact.Artifact) {
	for _, item := range artifacts {
		abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
		if err != nil {
			slog.Warn("artifact cleanup skipped unsafe path", "artifact_id", item.ID, "error", err)
			continue
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			slog.Warn("artifact cleanup failed", "artifact_id", item.ID, "error", err)
		}
	}
}
