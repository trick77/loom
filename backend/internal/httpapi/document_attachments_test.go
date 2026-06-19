package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/rag"
)

// inlineStub is a DocumentService that serves documents and full text by ID, for
// exercising the inline-attachment path.
type inlineStub struct {
	docs    map[string]rag.Document
	texts   map[string]string
	indexed []rag.IndexedDoc
}

func (s *inlineStub) Upload(context.Context, documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	return rag.Document{}, artifact.Artifact{}, nil
}
func (s *inlineStub) List(context.Context, string, *string) ([]rag.Document, error) { return nil, nil }
func (s *inlineStub) Get(_ context.Context, _, id string) (rag.Document, bool, error) {
	d, ok := s.docs[id]
	return d, ok, nil
}
func (s *inlineStub) FullText(_ context.Context, _, id string) (string, error) {
	return s.texts[id], nil
}
func (s *inlineStub) Index(context.Context, string, string) error             { return nil }
func (s *inlineStub) Unindex(context.Context, string, string) error           { return nil }
func (s *inlineStub) Delete(context.Context, string, string) error            { return nil }
func (s *inlineStub) DeleteThreadData(context.Context, string, string) error  { return nil }
func (s *inlineStub) DeleteProjectData(context.Context, string, string) error { return nil }
func (s *inlineStub) Retrieve(context.Context, string, *string, *string, string, int) ([]rag.RetrievedChunk, error) {
	return nil, nil
}
func (s *inlineStub) IndexedDocsInScope(context.Context, string, *string, *string) ([]rag.IndexedDoc, error) {
	return s.indexed, nil
}

func TestDocumentInlineContext_inlinesChatScopedDoc(t *testing.T) {
	stub := &inlineStub{
		docs:  map[string]rag.Document{"d1": {ID: "d1", Filename: "notes.md", ThreadID: strPtr("t1")}},
		texts: map[string]string{"d1": "Summarize me please."},
	}
	s := &server{documents: stub}
	thread := chat.Thread{ID: "t1"}

	block, inlined := s.documentInlineContext(context.Background(), "u1", thread, []string{"d1"})
	if !strings.Contains(block, "notes.md") || !strings.Contains(block, "Summarize me please.") {
		t.Fatalf("inline block missing content: %q", block)
	}
	if !strings.Contains(block, "<documents>") {
		t.Errorf("inline block missing delimiter: %q", block)
	}
	if !inlined["d1"] {
		t.Errorf("expected d1 to be marked inlined, got %v", inlined)
	}
}

func TestDocumentInlineContext_skipsOutOfScopeDoc(t *testing.T) {
	stub := &inlineStub{
		docs:  map[string]rag.Document{"d1": {ID: "d1", Filename: "x.md", ThreadID: strPtr("other")}},
		texts: map[string]string{"d1": "secret"},
	}
	s := &server{documents: stub}

	block, inlined := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1"})
	if block != "" || len(inlined) != 0 {
		t.Fatalf("out-of-scope doc must be skipped, got block=%q inlined=%v", block, inlined)
	}
}

func TestDocumentInlineContext_truncatesOversizedDoc(t *testing.T) {
	big := strings.Repeat("a", inlineDocByteBudget+1000)
	stub := &inlineStub{
		docs:  map[string]rag.Document{"d1": {ID: "d1", Filename: "big.txt", ThreadID: strPtr("t1")}},
		texts: map[string]string{"d1": big},
	}
	s := &server{documents: stub}

	block, inlined := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1"})
	// The model must still see the document's head this turn (never "nothing").
	if !strings.Contains(block, "big.txt") || !strings.Contains(block, "[… document truncated") {
		t.Fatalf("oversized doc should be inlined truncated, got %q", block)
	}
	// Truncated docs stay RAG-eligible, so they are NOT in the exclusion set.
	if inlined["d1"] {
		t.Errorf("truncated doc must remain RAG-eligible (not in inlined set), got %v", inlined)
	}
	if len(block) > inlineDocByteBudget {
		t.Errorf("block %d bytes exceeds budget %d", len(block), inlineDocByteBudget)
	}
}

func TestDocumentInlineContext_dedupesRepeatedID(t *testing.T) {
	stub := &inlineStub{
		docs:  map[string]rag.Document{"d1": {ID: "d1", Filename: "notes.md", ThreadID: strPtr("t1")}},
		texts: map[string]string{"d1": "UNIQUEMARKER content."},
	}
	s := &server{documents: stub}

	block, _ := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1", "d1"})
	if got := strings.Count(block, "UNIQUEMARKER"); got != 1 {
		t.Errorf("repeated id must be inlined once, got %d occurrences", got)
	}
}

func TestKnowledgeContext_excludesInlinedDocs(t *testing.T) {
	s := &server{documents: &stubDocs{chunks: []rag.RetrievedChunk{
		{DocumentID: "d1", Filename: "inlined.md", Text: "already inline"},
		{DocumentID: "d2", Filename: "other.md", Text: "fresh chunk"},
	}}}

	block, citations := s.knowledgeContextForThread(context.Background(), "u1", chat.Thread{ID: "t1"}, "q", map[string]bool{"d1": true})
	if strings.Contains(block, "inlined.md") || strings.Contains(block, "already inline") {
		t.Errorf("inlined doc must be excluded from RAG block: %q", block)
	}
	if !strings.Contains(block, "other.md") {
		t.Errorf("non-inlined doc should remain: %q", block)
	}
	for _, c := range citations {
		if c.DocumentID == "d1" {
			t.Errorf("citation for inlined doc d1 should be excluded: %+v", citations)
		}
	}
}
