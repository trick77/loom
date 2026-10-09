package turntest

import (
	"context"

	"github.com/trick77/loom/internal/artifact"
)

// ArtifactStore is an in-memory artifact store over the Artifacts slice.
type ArtifactStore struct {
	Artifacts []artifact.Artifact
	// Deleted, when set, records the ids passed to Delete (value receiver can't
	// mutate the slice, so deletions are tracked through this pointer instead).
	Deleted *[]string
	// Detached, when set, records the ids passed to DetachFromThread.
	Detached *[]string
	// CreateErr makes Create fail; Created, when set, receives Create's input.
	CreateErr error
	Created   *artifact.CreateInput
}

// DetachFromThread implements turn.ArtifactStore.
func (f ArtifactStore) DetachFromThread(_ context.Context, _ string, artifactIDs []string) error {
	if f.Detached != nil {
		*f.Detached = append(*f.Detached, artifactIDs...)
	}
	return nil
}

// Delete implements turn.ArtifactStore.
func (f ArtifactStore) Delete(_ context.Context, _ string, artifactID string) error {
	if f.Deleted != nil {
		*f.Deleted = append(*f.Deleted, artifactID)
	}
	return nil
}

// Rename implements turn.ArtifactStore.
func (f ArtifactStore) Rename(_ context.Context, userID, artifactID, displayFilename string) error {
	for i := range f.Artifacts {
		if f.Artifacts[i].UserID == userID && f.Artifacts[i].ID == artifactID {
			f.Artifacts[i].DisplayFilename = displayFilename
		}
	}
	return nil
}

// SetThumbnailRelPath implements turn.ArtifactStore.
func (f ArtifactStore) SetThumbnailRelPath(_ context.Context, userID, artifactID, relPath string) error {
	for i := range f.Artifacts {
		if f.Artifacts[i].UserID == userID && f.Artifacts[i].ID == artifactID {
			f.Artifacts[i].ThumbnailRelPath = relPath
		}
	}
	return nil
}

// GetMany implements turn.ArtifactStore.
func (f ArtifactStore) GetMany(_ context.Context, userID string, ids []string) (map[string]artifact.Artifact, error) {
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make(map[string]artifact.Artifact)
	for _, item := range f.Artifacts {
		if item.UserID != userID {
			continue
		}
		if _, ok := want[item.ID]; ok {
			out[item.ID] = item
		}
	}
	return out, nil
}

// Create implements turn.ArtifactStore.
func (f ArtifactStore) Create(_ context.Context, in artifact.CreateInput) (artifact.Artifact, error) {
	if f.CreateErr != nil {
		return artifact.Artifact{}, f.CreateErr
	}
	if f.Created != nil {
		*f.Created = in
	}
	return artifact.Artifact{
		ID:              "art_created",
		UserID:          in.UserID,
		ThreadID:        in.ThreadID,
		ProjectID:       in.ProjectID,
		DisplayFilename: in.DisplayFilename,
		VolumeRelPath:   in.VolumeRelPath,
		MIMEType:        in.MIMEType,
		SizeBytes:       in.SizeBytes,
	}, nil
}

// Get implements turn.ArtifactStore.
func (f ArtifactStore) Get(_ context.Context, userID, artifactID string) (artifact.Artifact, bool, error) {
	for _, item := range f.Artifacts {
		if item.UserID == userID && item.ID == artifactID {
			return item, true, nil
		}
	}
	return artifact.Artifact{}, false, nil
}

// List implements turn.ArtifactStore.
func (f ArtifactStore) List(_ context.Context, userID string, _ artifact.ListOptions) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.Artifacts {
		if item.UserID == userID {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListForThread implements turn.ArtifactStore.
func (f ArtifactStore) ListForThread(_ context.Context, _ string, threadID string) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.Artifacts {
		if item.ThreadID == threadID {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListForProject implements turn.ArtifactStore.
func (f ArtifactStore) ListForProject(_ context.Context, _ string, projectID string) ([]artifact.Artifact, error) {
	var out []artifact.Artifact
	for _, item := range f.Artifacts {
		if item.ProjectID != nil && *item.ProjectID == projectID {
			out = append(out, item)
		}
	}
	return out, nil
}
