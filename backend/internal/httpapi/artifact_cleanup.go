package httpapi

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/trick77/loom/internal/artifact"
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

// cleanupArtifactFiles removes the volume files of artifacts whose rows are gone,
// sidecar thumbnail included. Any id in keep is skipped: those artifacts outlived
// the delete (see detachArtifactsInUse) and their bytes are still referenced.
func (s *server) cleanupArtifactFiles(userID string, artifacts []artifact.Artifact, keep map[string]struct{}) {
	for _, item := range artifacts {
		if _, kept := keep[item.ID]; kept {
			continue
		}
		abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
		if err != nil {
			slog.Warn("artifact cleanup skipped unsafe path", "artifact_id", item.ID, "err", err)
			continue
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			slog.Warn("artifact cleanup failed", "artifact_id", item.ID, "err", err)
		}
		// The sidecar thumbnail lives outside the artifact's own relpath, so the
		// remove above never reaches it; without this every image artifact in a
		// deleted thread or project leaves a JPEG behind under .loom/thumbnails.
		artifact.RemoveThumbnail(s.usersDir, userID, item.VolumeRelPath)
	}
}

// detachArtifactsInUse clears the thread id on the thread's artifacts that still
// back a document outliving it (a project or user-global knowledge document
// uploaded from this chat, whose artifact carries the thread id as provenance
// only). Without this the thread's cascade reaches those artifact rows, which
// fires documents' ON DELETE SET NULL over the composite (user_id, artifact_id)
// key; that nulls user_id too and aborts the whole delete on its NOT NULL
// constraint, so the thread cannot be deleted at all. It returns the ids to spare
// from the file cleanup, whose bytes the surviving document still needs.
//
// Runs after the thread's own documents are deleted, so only true survivors match.
func (s *server) detachArtifactsInUse(ctx context.Context, userID, threadID string) (map[string]struct{}, error) {
	if s.documents == nil || s.artifacts == nil {
		return nil, nil
	}
	ids, err := s.documents.ArtifactIDsForThreadArtifactsInUse(ctx, userID, threadID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := s.artifacts.DetachFromThread(ctx, userID, ids); err != nil {
		return nil, err
	}
	keep := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	return keep, nil
}
