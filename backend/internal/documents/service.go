package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/rag"
)

// ErrUnsupportedFormat is returned when an upload's extension is not allowlisted.
var ErrUnsupportedFormat = errors.New("unsupported document format")

// ArtifactStore is the subset of the artifact store the document service needs.
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
	Delete(context.Context, string, string) error
}

// Indexer runs the ingest pipeline for one document (implemented by *rag.Ingester).
type Indexer interface {
	Ingest(ctx context.Context, userID, documentID string) error
}

// Service ties uploads to the volume, the artifact store, the RAG store, and the
// ingest pipeline. It is the application-level entry point for document handlers.
type Service struct {
	store     *rag.Store
	artifacts ArtifactStore
	indexer   Indexer
	embedder  rag.Embedder
	usersDir  string
}

func NewService(store *rag.Store, artifacts ArtifactStore, indexer Indexer, embedder rag.Embedder, usersDir string) *Service {
	return &Service{store: store, artifacts: artifacts, indexer: indexer, embedder: embedder, usersDir: usersDir}
}

// UploadInput describes a single document upload. ThreadID may be empty for a
// global (Artifacts-browser) upload; ProjectID scopes the document for retrieval.
type UploadInput struct {
	UserID    string
	ThreadID  string
	ProjectID *string
	Filename  string
	Reader    io.Reader
}

// Upload validates the format, writes the file into the user's volume, records an
// artifact (source=user_uploaded), and creates a pending document. It does NOT
// index; call Index ("Add to knowledge") for that.
func (s *Service) Upload(ctx context.Context, in UploadInput) (rag.Document, artifact.Artifact, error) {
	mime, ok := AllowedFormat(in.Filename)
	if !ok {
		return rag.Document{}, artifact.Artifact{}, ErrUnsupportedFormat
	}
	ext := strings.TrimPrefix(filepath.Ext(in.Filename), ".")

	out, file, err := artifact.CreateUploadFile(artifact.UploadRequest{
		UsersDir:        s.usersDir,
		UserID:          in.UserID,
		ProjectID:       in.ProjectID,
		DisplayFilename: in.Filename,
		Extension:       ext,
	})
	if err != nil {
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("create upload file: %w", err)
	}
	size, copyErr := io.Copy(file, in.Reader)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("write upload: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("close upload: %w", closeErr)
	}

	art, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          in.UserID,
		ThreadID:        in.ThreadID,
		ProjectID:       in.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        mime,
		SizeBytes:       size,
		Source:          "user_uploaded",
	})
	if err != nil {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("record artifact: %w", err)
	}

	doc := rag.Document{
		ID:            chat.NewIDForInternalUse(),
		UserID:        in.UserID,
		ProjectID:     in.ProjectID,
		ArtifactID:    &art.ID,
		VolumeRelpath: out.VolumeRelPath,
		Filename:      out.DisplayFilename,
		MIME:          mime,
		SizeBytes:     size,
		Status:        rag.StatusPending,
	}
	if err := s.store.CreateDocument(ctx, doc); err != nil {
		os.Remove(out.AbsPath)
		_ = s.artifacts.Delete(ctx, in.UserID, art.ID)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("create document: %w", err)
	}
	return doc, art, nil
}

// Index runs ingestion for a document ("Add to knowledge"). Callers that want it
// off the request path should invoke it in a detached goroutine.
func (s *Service) Index(ctx context.Context, userID, documentID string) error {
	return s.indexer.Ingest(ctx, userID, documentID)
}

func (s *Service) List(ctx context.Context, userID string, projectID *string) ([]rag.Document, error) {
	docs, err := s.store.ListDocuments(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	// Light reconciliation: a document whose file vanished from the volume is
	// marked stale and excluded from retrieval. Only check states that imply the
	// file should exist, to keep listing cheap.
	for i := range docs {
		d := &docs[i]
		if d.Status == rag.StatusStale || d.Status == rag.StatusError {
			continue
		}
		abs, err := artifact.ResolveExisting(s.usersDir, userID, d.VolumeRelpath)
		if err != nil {
			continue
		}
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			if updErr := s.store.UpdateStatus(ctx, userID, d.ID, rag.StatusStale, "file missing from volume"); updErr == nil {
				d.Status = rag.StatusStale
				d.Error = "file missing from volume"
			}
		}
	}
	return docs, nil
}

func (s *Service) Get(ctx context.Context, userID, documentID string) (rag.Document, bool, error) {
	return s.store.GetDocument(ctx, userID, documentID)
}

// Unindex removes a document's chunks/embeddings but keeps the file and document
// row (status back to pending), so it can be re-indexed later.
func (s *Service) Unindex(ctx context.Context, userID, documentID string) error {
	if err := s.store.ClearChunks(ctx, userID, documentID); err != nil {
		return err
	}
	return s.store.UpdateStatus(ctx, userID, documentID, rag.StatusPending, "")
}

// Delete removes the document, its chunks/embeddings, its artifact row, and the
// underlying file from the volume.
func (s *Service) Delete(ctx context.Context, userID, documentID string) error {
	doc, ok, err := s.store.GetDocument(ctx, userID, documentID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := s.store.DeleteDocument(ctx, userID, documentID); err != nil {
		return err
	}
	if doc.ArtifactID != nil {
		_ = s.artifacts.Delete(ctx, userID, *doc.ArtifactID)
	}
	if abs, err := artifact.ResolveExisting(s.usersDir, userID, doc.VolumeRelpath); err == nil {
		_ = os.Remove(abs)
	}
	return nil
}

// Retrieve embeds the query and returns the most relevant chunks for the user's
// knowledge scope. It is best-effort for callers: an embedding failure surfaces
// as an error the caller may choose to ignore.
func (s *Service) Retrieve(ctx context.Context, userID string, projectID *string, query string, k int) ([]rag.RetrievedChunk, error) {
	// Avoid an embedding round-trip on every chat turn when the user has nothing
	// indexed in scope.
	if has, err := s.store.HasIndexedChunks(ctx, userID, projectID); err != nil {
		return nil, err
	} else if !has {
		return nil, nil
	}
	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	return s.store.Retrieve(ctx, userID, projectID, vecs[0], k)
}
