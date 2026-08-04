package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/inference"
	"github.com/trick77/loom/internal/rag"
)

// attributionStub is a DocumentService that records the inference metadata the
// turn's retrieval/extraction calls run under.
type attributionStub struct {
	retrieveMetadata inference.Metadata
	fullTextMetadata inference.Metadata
	indexed          []rag.IndexedDoc
	docs             map[string]rag.Document
}

func (s *attributionStub) Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	return rag.Document{}, artifact.Artifact{}, nil
}
func (s *attributionStub) List(context.Context, string, *string) ([]rag.Document, error) {
	return nil, nil
}
func (s *attributionStub) Get(_ context.Context, _, id string) (rag.Document, bool, error) {
	d, ok := s.docs[id]
	return d, ok, nil
}
func (s *attributionStub) FullText(ctx context.Context, _, _ string) (string, error) {
	s.fullTextMetadata = inference.MetadataFromContext(ctx)
	return "the document text", nil
}
func (s *attributionStub) Index(context.Context, string, string) error             { return nil }
func (s *attributionStub) Unindex(context.Context, string, string) error           { return nil }
func (s *attributionStub) Delete(context.Context, string, string) error            { return nil }
func (s *attributionStub) DeleteThreadData(context.Context, string, string) error  { return nil }
func (s *attributionStub) DeleteProjectData(context.Context, string, string) error { return nil }
func (s *attributionStub) Retrieve(ctx context.Context, _ string, _, _ *string, _ string, _ int) ([]rag.RetrievedChunk, error) {
	s.retrieveMetadata = inference.MetadataFromContext(ctx)
	return nil, nil
}
func (s *attributionStub) IndexedDocsInScope(context.Context, string, *string, *string) ([]rag.IndexedDoc, error) {
	return s.indexed, nil
}

// The RAG query embedding and the document extraction (which describes an
// attached image via the vision model) run during prompt assembly, before the
// chat calls attach their own metadata. They must still be attributed, or their
// inference log lines cannot be tied back to a user — the same defect the image
// gate had when it logged without a username.
func TestStreamMessage_attributesPromptAssemblyModelCalls(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	docs := &attributionStub{
		docs: map[string]rag.Document{"d1": {ID: "d1", Filename: "photo.png", ThreadID: strPtr("thr_1")}},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		LLM:       fakeChatClient{},
		Documents: docs,
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream",
		`{"content":"What is in this?","documentAttachmentIds":["d1"]}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	for name, got := range map[string]inference.Metadata{
		"Retrieve": docs.retrieveMetadata,
		"FullText": docs.fullTextMetadata,
	} {
		if got.UserID != testUser.ID {
			t.Errorf("%s ran with user_id %q, want %q", name, got.UserID, testUser.ID)
		}
		if got.Username != testUser.Username {
			t.Errorf("%s ran with username %q, want %q", name, got.Username, testUser.Username)
		}
		if got.ThreadID != "thr_1" {
			t.Errorf("%s ran with thread_id %q, want thr_1", name, got.ThreadID)
		}
	}
}
