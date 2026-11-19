package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// SourceUserUploaded marks an artifact the user uploaded, as opposed to one
// the assistant generated.
const SourceUserUploaded = "user_uploaded"

// CountThreadUploads counts the user's own thread-private uploads among a
// thread's artifacts: the number the per-chat attachment limit applies to.
// Project-scoped uploads and generated artifacts do not count.
func CountThreadUploads(items []Artifact, userID string) int {
	count := 0
	for _, item := range items {
		if item.UserID == userID && item.ProjectID == nil && item.Source == SourceUserUploaded {
			count++
		}
	}
	return count
}

// ErrThreadUploadLimit is returned by StoreUpload when the thread already holds
// ThreadLimit private uploads.
var ErrThreadUploadLimit = errors.New("thread upload limit reached")

// ErrUploadTooLarge is returned by StoreUpload when the content exceeds
// MaxArtifactSizeBytes.
var ErrUploadTooLarge = errors.New("upload too large")

// UploadStage names the StoreUpload step an UploadError failed in, so callers
// can map each to its own response (a bad upload path is the client's fault,
// a failed write is not).
type UploadStage string

// The StoreUpload steps that can fail with an UploadError.
const (
	UploadStageCount  UploadStage = "count thread uploads"
	UploadStageFile   UploadStage = "create upload file"
	UploadStageWrite  UploadStage = "write upload"
	UploadStageRecord UploadStage = "record artifact"
)

// UploadError is a StoreUpload failure in one step.
type UploadError struct {
	Stage UploadStage
	Err   error
}

func (e *UploadError) Error() string { return string(e.Stage) + ": " + e.Err.Error() }

func (e *UploadError) Unwrap() error { return e.Err }

// UploadStore is the part of the artifact store StoreUpload needs.
type UploadStore interface {
	ListForThread(context.Context, string, string) ([]Artifact, error)
	Create(context.Context, CreateInput) (Artifact, error)
}

// StoreUploadInput describes one user upload. The caller has validated the
// format and picked MIMEType and Extension.
type StoreUploadInput struct {
	UsersDir        string
	UserID          string
	ThreadID        string
	ProjectID       *string
	DisplayFilename string
	Extension       string
	MIMEType        string
	Reader          io.Reader
	// ThreadLimit caps a project-less thread's private uploads; 0 disables it.
	ThreadLimit int
	// Thumbnail writes the sidecar thumbnail before the row is recorded.
	Thumbnail bool
}

// StoredUpload is a written and recorded upload. Path and Size come from the
// write itself, so a caller that records more rows can clean the file up if
// its own step fails.
type StoredUpload struct {
	Artifact Artifact
	Path     OutputPath
	Size     int64
}

// StoreUpload writes an upload into the user's volume and records its artifact
// (source user_uploaded). A failure leaves neither the file nor its thumbnail
// behind.
func StoreUpload(ctx context.Context, store UploadStore, in StoreUploadInput) (StoredUpload, error) {
	if in.ThreadLimit > 0 && in.ProjectID == nil && in.ThreadID != "" {
		items, err := store.ListForThread(ctx, in.UserID, in.ThreadID)
		if err != nil {
			return StoredUpload{}, &UploadError{Stage: UploadStageCount, Err: err}
		}
		if CountThreadUploads(items, in.UserID) >= in.ThreadLimit {
			return StoredUpload{}, ErrThreadUploadLimit
		}
	}
	out, file, err := CreateUploadFile(UploadRequest{
		UsersDir:        in.UsersDir,
		UserID:          in.UserID,
		ProjectID:       in.ProjectID,
		DisplayFilename: in.DisplayFilename,
		Extension:       in.Extension,
	})
	if err != nil {
		return StoredUpload{}, &UploadError{Stage: UploadStageFile, Err: err}
	}
	// The limit applies to the content, not the multipart envelope the
	// handlers' MaxBytesReader lets through on top.
	size, copyErr := io.Copy(file, io.LimitReader(in.Reader, MaxArtifactSizeBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(out.AbsPath)
		return StoredUpload{}, &UploadError{Stage: UploadStageWrite, Err: copyErr}
	}
	if closeErr != nil {
		_ = os.Remove(out.AbsPath)
		return StoredUpload{}, &UploadError{Stage: UploadStageWrite, Err: fmt.Errorf("close: %w", closeErr)}
	}
	if size > MaxArtifactSizeBytes {
		_ = os.Remove(out.AbsPath)
		return StoredUpload{}, ErrUploadTooLarge
	}
	var thumbnailRelPath string
	if in.Thumbnail {
		if src, err := os.ReadFile(out.AbsPath); err == nil {
			thumbnailRelPath = GenerateThumbnailBestEffort(in.UsersDir, in.UserID, in.MIMEType, src, out.VolumeRelPath)
		}
	}
	created, err := store.Create(ctx, CreateInput{
		UserID:           in.UserID,
		ThreadID:         in.ThreadID,
		ProjectID:        in.ProjectID,
		DisplayFilename:  out.DisplayFilename,
		VolumeRelPath:    out.VolumeRelPath,
		MIMEType:         in.MIMEType,
		SizeBytes:        size,
		Source:           SourceUserUploaded,
		ThumbnailRelPath: thumbnailRelPath,
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		if in.Thumbnail {
			RemoveThumbnail(in.UsersDir, in.UserID, out.VolumeRelPath)
		}
		return StoredUpload{}, &UploadError{Stage: UploadStageRecord, Err: err}
	}
	return StoredUpload{Artifact: created, Path: out, Size: size}, nil
}
