package httpapi

import (
	"context"
	"fmt"
	"os"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
)

// artifactSpec describes bytes to persist as a generated artifact.
type artifactSpec struct {
	DisplayFilename string
	Extension       string
	// MIMEType overrides the type derived from the extension when the producer
	// knows better (the image provider reports the actual encoding).
	MIMEType string
	Data     []byte
	// Thumbnail asks for the eager sidecar thumbnail (best-effort, raster only).
	Thumbnail bool
}

// persistArtifactBytes writes spec.Data into the user's volume under a
// collision-free name and records the artifact row. The file, and the
// thumbnail if one was made, are removed again when any later step fails, so
// a failed persist leaves nothing behind on disk. This is the one path for
// every generated artifact (documents, images, extracted code); uploads go
// through artifact.CreateUploadFile.
func (s *server) persistArtifactBytes(ctx context.Context, user auth.User, thread chat.Thread, spec artifactSpec) (artifact.Artifact, error) {
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: spec.DisplayFilename,
		Extension:       spec.Extension,
	})
	if err != nil {
		return artifact.Artifact{}, err
	}
	if _, err := file.Write(spec.Data); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return artifact.Artifact{}, fmt.Errorf("write artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return artifact.Artifact{}, fmt.Errorf("close artifact: %w", err)
	}
	mimeType := out.MIMEType
	if spec.MIMEType != "" {
		mimeType = spec.MIMEType
	}
	thumbnailRelPath := ""
	if spec.Thumbnail {
		thumbnailRelPath = generateThumbnailBestEffort(s.usersDir, user.ID, mimeType, spec.Data, out.VolumeRelPath)
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:           user.ID,
		ThreadID:         thread.ID,
		ProjectID:        thread.ProjectID,
		DisplayFilename:  out.DisplayFilename,
		VolumeRelPath:    out.VolumeRelPath,
		MIMEType:         mimeType,
		SizeBytes:        int64(len(spec.Data)),
		ThumbnailRelPath: thumbnailRelPath,
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		if thumbnailRelPath != "" {
			artifact.RemoveThumbnail(s.usersDir, user.ID, out.VolumeRelPath)
		}
		return artifact.Artifact{}, fmt.Errorf("persist artifact: %w", err)
	}
	return created, nil
}
