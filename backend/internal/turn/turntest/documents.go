package turntest

import (
	"context"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/rag"
)

// DocumentService is a document service over one document that records the
// uploads, deletions and index calls it receives.
type DocumentService struct {
	Uploaded           documents.UploadInput
	UploadErr          error
	Doc                rag.Document
	Text               string
	FullTextErr        error
	DeletedThreadData  []string
	DeletedProjectData []string
	DeleteDataErr      error
	// ArtifactsInUse is what ArtifactIDsForThreadArtifactsInUse returns, i.e. the
	// thread artifacts that still back a surviving document.
	ArtifactsInUse      []string
	ArtifactsInUseErr   error
	InUseQueriedThreads []string
	// FullTextEntered, when set, is signalled (non-blocking) when FullText runs.
	FullTextEntered chan struct{}
	DeleteErr       error
	// BackingArtifactID is the artifact behind the one document this fake knows;
	// DeleteForArtifact records the artifacts it deleted a document for.
	BackingArtifactID  string
	DeletedForArtifact []string
	// IndexCalls, when set, receives the id of every document Index runs for.
	IndexCalls chan string
}

// Upload implements turn.DocumentService.
func (f *DocumentService) Upload(_ context.Context, in documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	f.Uploaded = in
	if f.UploadErr != nil {
		return rag.Document{}, artifact.Artifact{}, f.UploadErr
	}
	return f.Doc, artifact.Artifact{}, nil
}

// List implements turn.DocumentService.
func (f *DocumentService) List(context.Context, string, *string) ([]rag.Document, error) {
	return []rag.Document{f.Doc}, nil
}

// Get implements turn.DocumentService.
func (f *DocumentService) Get(context.Context, string, string) (rag.Document, bool, error) {
	return f.Doc, true, nil
}

// FullText implements turn.DocumentService.
func (f *DocumentService) FullText(context.Context, string, string) (string, error) {
	if f.FullTextEntered != nil {
		select {
		case f.FullTextEntered <- struct{}{}:
		default:
		}
	}
	return f.Text, f.FullTextErr
}

// Index implements turn.DocumentService.
func (f *DocumentService) Index(_ context.Context, _, documentID string) error {
	if f.IndexCalls != nil {
		f.IndexCalls <- documentID
	}
	return nil
}

// Delete implements turn.DocumentService.
func (f *DocumentService) Delete(context.Context, string, string) error { return f.DeleteErr }

// DeleteForArtifact implements turn.DocumentService.
func (f *DocumentService) DeleteForArtifact(_ context.Context, _ string, artifactID string) (bool, error) {
	if f.BackingArtifactID == "" || artifactID != f.BackingArtifactID {
		return false, nil
	}
	f.DeletedForArtifact = append(f.DeletedForArtifact, artifactID)
	return true, f.DeleteErr
}

// DeleteThreadData implements turn.DocumentService.
func (f *DocumentService) DeleteThreadData(_ context.Context, _ string, threadID string) error {
	f.DeletedThreadData = append(f.DeletedThreadData, threadID)
	return f.DeleteDataErr
}

// ArtifactIDsForThreadArtifactsInUse implements turn.DocumentService.
func (f *DocumentService) ArtifactIDsForThreadArtifactsInUse(_ context.Context, _ string, threadID string) ([]string, error) {
	f.InUseQueriedThreads = append(f.InUseQueriedThreads, threadID)
	return f.ArtifactsInUse, f.ArtifactsInUseErr
}

// DeleteProjectData implements turn.DocumentService.
func (f *DocumentService) DeleteProjectData(_ context.Context, _ string, projectID string) error {
	f.DeletedProjectData = append(f.DeletedProjectData, projectID)
	return f.DeleteDataErr
}

// Retrieve implements turn.DocumentService.
func (f *DocumentService) Retrieve(context.Context, string, *string, *string, string, int) ([]rag.RetrievedChunk, error) {
	return nil, nil
}

// DocumentsInScope implements turn.DocumentService.
func (f *DocumentService) DocumentsInScope(context.Context, string, *string, *string, int) ([]rag.Document, error) {
	return []rag.Document{f.Doc}, nil
}

// IndexedDocsInScope implements turn.DocumentService.
func (f *DocumentService) IndexedDocsInScope(context.Context, string, *string, *string) ([]rag.IndexedDoc, error) {
	return nil, nil
}

// RetrievingDocuments is a document service whose Retrieve returns Chunks and
// Err and records the project id it was asked for.
type RetrievingDocuments struct {
	Chunks []rag.RetrievedChunk
	Err    error
	GotPID *string
}

// Upload implements turn.DocumentService.
func (s *RetrievingDocuments) Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	return rag.Document{}, artifact.Artifact{}, nil
}

// List implements turn.DocumentService.
func (s *RetrievingDocuments) List(context.Context, string, *string) ([]rag.Document, error) {
	return nil, nil
}

// Get implements turn.DocumentService.
func (s *RetrievingDocuments) Get(context.Context, string, string) (rag.Document, bool, error) {
	return rag.Document{}, false, nil
}

// FullText implements turn.DocumentService.
func (s *RetrievingDocuments) FullText(context.Context, string, string) (string, error) {
	return "", nil
}

// Index implements turn.DocumentService.
func (s *RetrievingDocuments) Index(context.Context, string, string) error { return nil }

// Delete implements turn.DocumentService.
func (s *RetrievingDocuments) Delete(context.Context, string, string) error { return nil }

// DeleteForArtifact implements turn.DocumentService.
func (s *RetrievingDocuments) DeleteForArtifact(context.Context, string, string) (bool, error) {
	return false, nil
}

// DeleteThreadData implements turn.DocumentService.
func (s *RetrievingDocuments) DeleteThreadData(context.Context, string, string) error { return nil }

// ArtifactIDsForThreadArtifactsInUse implements turn.DocumentService.
func (s *RetrievingDocuments) ArtifactIDsForThreadArtifactsInUse(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// DeleteProjectData implements turn.DocumentService.
func (s *RetrievingDocuments) DeleteProjectData(context.Context, string, string) error { return nil }

// DocumentsInScope implements turn.DocumentService.
func (s *RetrievingDocuments) DocumentsInScope(context.Context, string, *string, *string, int) ([]rag.Document, error) {
	return nil, nil
}

// IndexedDocsInScope implements turn.DocumentService.
func (s *RetrievingDocuments) IndexedDocsInScope(context.Context, string, *string, *string) ([]rag.IndexedDoc, error) {
	return nil, nil
}

// Retrieve implements turn.DocumentService.
func (s *RetrievingDocuments) Retrieve(_ context.Context, _ string, projectID *string, _ *string, _ string, _ int) ([]rag.RetrievedChunk, error) {
	s.GotPID = projectID
	return s.Chunks, s.Err
}

// ListingDocuments is a document service whose List returns several documents.
type ListingDocuments struct {
	DocumentService
	Docs  []rag.Document
	Extra []rag.Document // found by Get only, outside the scope listing
}

// DocumentsInScope implements turn.DocumentService.
func (l *ListingDocuments) DocumentsInScope(context.Context, string, *string, *string, int) ([]rag.Document, error) {
	return l.Docs, nil
}

// Get implements turn.DocumentService.
func (l *ListingDocuments) Get(_ context.Context, _ string, id string) (rag.Document, bool, error) {
	for _, d := range l.Docs {
		if d.ID == id {
			return d, true, nil
		}
	}
	for _, d := range l.Extra {
		if d.ID == id {
			return d, true, nil
		}
	}
	return rag.Document{}, false, nil
}
