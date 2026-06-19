package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/rag"
)

const MaxChatDocuments = 10

// ErrUnsupportedFormat is returned when an upload's extension is not allowlisted.
var ErrUnsupportedFormat = errors.New("unsupported document format")

// ErrChatDocumentLimit is returned when a project-less chat already has the
// maximum number of private uploaded attachments.
var ErrChatDocumentLimit = errors.New("chat document limit reached")

// ErrTooLarge is returned when an upload's content exceeds
// artifact.MaxArtifactSizeBytes. The content size (not the multipart envelope)
// is the enforced limit, mirroring the image attachment handler.
var ErrTooLarge = errors.New("upload too large")

// ArtifactStore is the subset of the artifact store the document service needs.
type ArtifactStore interface {
	Create(context.Context, artifact.CreateInput) (artifact.Artifact, error)
	Get(context.Context, string, string) (artifact.Artifact, bool, error)
	ListForThread(context.Context, string, string) ([]artifact.Artifact, error)
	Delete(context.Context, string, string) error
}

// Indexer runs the ingest pipeline for one document (implemented by *rag.Ingester).
type Indexer interface {
	Ingest(ctx context.Context, userID, documentID string) error
	ExtractText(ctx context.Context, userID, documentID string) (string, error)
}

type UsageRecorder interface {
	AddEmbeddingUsage(ctx context.Context, userID string, tokens, requests int) error
}

// Service ties uploads to the volume, the artifact store, the RAG store, and the
// ingest pipeline. It is the application-level entry point for document handlers.
type Service struct {
	store     *rag.Store
	artifacts ArtifactStore
	indexer   Indexer
	embedder  rag.Embedder
	usage     UsageRecorder
	usersDir  string
}

func NewService(store *rag.Store, artifacts ArtifactStore, indexer Indexer, embedder rag.Embedder, usersDir string) *Service {
	return &Service{store: store, artifacts: artifacts, indexer: indexer, embedder: embedder, usersDir: usersDir}
}

func (s *Service) SetUsageRecorder(usage UsageRecorder) {
	s.usage = usage
}

// UploadInput describes a single document upload. ProjectID scopes the document
// to a project; a project-less upload with a ThreadID is private to that chat.
// A project-less upload with an empty ThreadID would be user-global, but no
// caller does that today (the composer always passes its thread) — such globals
// are treated as a legacy state and are cleaned up by
// rag.ReconcileLegacyDocumentScopes.
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
	if in.ProjectID == nil && strings.TrimSpace(in.ThreadID) != "" {
		threadID := strings.TrimSpace(in.ThreadID)
		documentCount, err := s.store.CountThreadDocuments(ctx, in.UserID, threadID)
		if err != nil {
			return rag.Document{}, artifact.Artifact{}, fmt.Errorf("count chat documents: %w", err)
		}
		uploadCount, err := s.countThreadUploads(ctx, in.UserID, threadID)
		if err != nil {
			return rag.Document{}, artifact.Artifact{}, fmt.Errorf("count chat uploads: %w", err)
		}
		if documentCount >= MaxChatDocuments || uploadCount >= MaxChatDocuments {
			return rag.Document{}, artifact.Artifact{}, ErrChatDocumentLimit
		}
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
	// Enforce the size limit on file content, tolerating multipart envelope
	// overhead the handler's MaxBytesReader lets through (mirrors the image
	// attachment handler so both paths share one effective content limit).
	size, copyErr := io.Copy(file, io.LimitReader(in.Reader, artifact.MaxArtifactSizeBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("write upload: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, fmt.Errorf("close upload: %w", closeErr)
	}
	if size > artifact.MaxArtifactSizeBytes {
		os.Remove(out.AbsPath)
		return rag.Document{}, artifact.Artifact{}, ErrTooLarge
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

	// A composer upload in a project-less chat is private to that one thread; a
	// project upload keeps the project scope (ThreadID is then provenance-only).
	// A project-less upload with no thread stays user-global — a legacy-only state
	// no caller produces today (see UploadInput).
	var threadID *string
	if in.ProjectID == nil && in.ThreadID != "" {
		threadID = &in.ThreadID
	}

	doc := rag.Document{
		ID:            chat.NewIDForInternalUse(),
		UserID:        in.UserID,
		ProjectID:     in.ProjectID,
		ThreadID:      threadID,
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

func (s *Service) countThreadUploads(ctx context.Context, userID, threadID string) (int, error) {
	items, err := s.artifacts.ListForThread(ctx, userID, threadID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if item.UserID == userID && item.ProjectID == nil && item.Source == "user_uploaded" {
			count++
		}
	}
	return count, nil
}

// Index runs ingestion for a document ("Add to knowledge"). Callers that want it
// off the request path should invoke it in a detached goroutine.
func (s *Service) Index(ctx context.Context, userID, documentID string) error {
	return s.indexer.Ingest(ctx, userID, documentID)
}

// FullText returns a document's full plain text (re-extracted from the volume),
// for inlining an attached document into the chat prompt. It does not require the
// document to be indexed and leaves its RAG state untouched. For an image
// document the text is the vision description: served from the cache populated at
// ingest when the image was indexed, otherwise a live (timeout-bounded)
// DescribeImage call — the same live-extraction shape used for unindexed
// non-image attachments, just with a model call instead of Tika.
func (s *Service) FullText(ctx context.Context, userID, documentID string) (string, error) {
	return s.indexer.ExtractText(ctx, userID, documentID)
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

// IndexedDocsInScope returns the embedded documents in the thread's knowledge
// scope with their token counts, for deciding which to inject in full.
func (s *Service) IndexedDocsInScope(ctx context.Context, userID string, projectID, threadID *string) ([]rag.IndexedDoc, error) {
	return s.store.IndexedDocsInScope(ctx, userID, projectID, threadID)
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

// DeleteThreadData removes all RAG data (documents, chunks, embeddings) scoped
// to a deleted chat. Files on disk are cleaned up separately by the caller via
// the artifact cleanup routine. Call before the thread row is deleted.
func (s *Service) DeleteThreadData(ctx context.Context, userID, threadID string) error {
	return s.store.DeleteThreadScopeDocuments(ctx, userID, threadID)
}

// DeleteProjectData removes all RAG data scoped to a deleted project. It MUST be
// called before chat.DeleteProject, whose FK cascade would otherwise drop the
// chunk rows and orphan the vec0 embeddings.
func (s *Service) DeleteProjectData(ctx context.Context, userID, projectID string) error {
	return s.store.DeleteProjectScopeDocuments(ctx, userID, projectID)
}

// Retrieve embeds the query and returns the most relevant chunks for the user's
// knowledge scope. It is best-effort for callers: an embedding failure surfaces
// as an error the caller may choose to ignore.
func (s *Service) Retrieve(ctx context.Context, userID string, projectID, threadID *string, query string, k int) ([]rag.RetrievedChunk, error) {
	// Avoid an embedding round-trip on every chat turn when the user has nothing
	// indexed in scope.
	if has, err := s.store.HasIndexedChunks(ctx, userID, projectID, threadID); err != nil {
		return nil, err
	} else if !has {
		return nil, nil
	}
	result, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(result.Vectors) == 0 {
		return nil, nil
	}
	s.recordEmbeddingUsage(ctx, userID, result.Usage)
	return s.store.Retrieve(ctx, userID, projectID, threadID, result.Vectors[0], k)
}

func (s *Service) recordEmbeddingUsage(ctx context.Context, userID string, u rag.EmbeddingUsage) {
	if s.usage == nil || !u.Present {
		return
	}
	_ = s.usage.AddEmbeddingUsage(ctx, userID, u.TotalTokens, 1)
}
