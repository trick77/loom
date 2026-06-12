package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/documents"
	"github.com/trick77/slopr/internal/rag"
)

type stubDocs struct {
	chunks []rag.RetrievedChunk
	err    error
	gotPID *string
}

func (s *stubDocs) Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	return rag.Document{}, artifact.Artifact{}, nil
}
func (s *stubDocs) List(context.Context, string, *string) ([]rag.Document, error) { return nil, nil }
func (s *stubDocs) Get(context.Context, string, string) (rag.Document, bool, error) {
	return rag.Document{}, false, nil
}
func (s *stubDocs) Index(context.Context, string, string) error   { return nil }
func (s *stubDocs) Unindex(context.Context, string, string) error { return nil }
func (s *stubDocs) Delete(context.Context, string, string) error  { return nil }
func (s *stubDocs) Retrieve(_ context.Context, _ string, projectID *string, _ *string, _ string, _ int) ([]rag.RetrievedChunk, error) {
	s.gotPID = projectID
	return s.chunks, s.err
}

func TestKnowledgeContext_buildsBlockAndSources(t *testing.T) {
	s := &server{documents: &stubDocs{chunks: []rag.RetrievedChunk{
		{DocumentID: "d1", Filename: "guide.pdf", Text: "Install with make build."},
		{DocumentID: "d1", Filename: "guide.pdf", Text: "Run make test."},
		{DocumentID: "d2", Filename: "notes.md", Text: "Remember the API key."},
	}}}
	thread := chat.Thread{ID: "t1"}

	block, citations := s.knowledgeContextForThread(context.Background(), "u1", thread, "how do I build")
	if !strings.Contains(block, "guide.pdf") || !strings.Contains(block, "Install with make build.") {
		t.Errorf("knowledge block missing content: %q", block)
	}
	// AnythingLLM-style: one citation per retrieved chunk (frontend groups them).
	if len(citations) != 3 {
		t.Fatalf("citations = %d, want 3 (one per chunk)", len(citations))
	}
	if citations[0].Filename != "guide.pdf" || citations[0].Snippet == "" || citations[0].Score <= 0 {
		t.Errorf("citation[0] = %+v, want filename/snippet/score populated", citations[0])
	}
}

func TestKnowledgeContext_passesProjectScope(t *testing.T) {
	stub := &stubDocs{}
	s := &server{documents: stub}
	pid := "p1"
	thread := chat.Thread{ID: "t1", ProjectID: &pid}
	s.knowledgeContextForThread(context.Background(), "u1", thread, "q")
	if stub.gotPID == nil || *stub.gotPID != "p1" {
		t.Errorf("retrieve project scope = %v, want p1", stub.gotPID)
	}
}

func TestKnowledgeContext_bestEffortOnError(t *testing.T) {
	s := &server{documents: &stubDocs{err: errors.New("embed down")}}
	block, sources := s.knowledgeContextForThread(context.Background(), "u1", chat.Thread{ID: "t1"}, "q")
	if block != "" || sources != nil {
		t.Errorf("on error want empty block/sources, got %q / %v", block, sources)
	}
}

func TestKnowledgeContext_disabledWhenNoService(t *testing.T) {
	s := &server{}
	if block, sources := s.knowledgeContextForThread(context.Background(), "u1", chat.Thread{ID: "t1"}, "q"); block != "" || sources != nil {
		t.Errorf("want empty when documents disabled, got %q / %v", block, sources)
	}
}
