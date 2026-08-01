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

	block, inlined, _ := s.documentInlineContext(context.Background(), "u1", thread, []string{"d1"}, newDocIndexer())
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

	block, inlined, _ := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1"}, newDocIndexer())
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

	block, inlined, _ := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1"}, newDocIndexer())
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

	block, _, _ := s.documentInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, []string{"d1", "d1"}, newDocIndexer())
	if got := strings.Count(block, "UNIQUEMARKER"); got != 1 {
		t.Errorf("repeated id must be inlined once, got %d occurrences", got)
	}
}

func TestKnowledgeContext_excludesInlinedDocs(t *testing.T) {
	s := &server{documents: &stubDocs{chunks: []rag.RetrievedChunk{
		{DocumentID: "d1", Filename: "inlined.md", Text: "already inline"},
		{DocumentID: "d2", Filename: "other.md", Text: "fresh chunk"},
	}}}

	block, citations := s.knowledgeContextForThread(context.Background(), "u1", chat.Thread{ID: "t1"}, "q", map[string]bool{"d1": true}, newDocIndexer())
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

// An attached document must be numbered and cited like any other source. Without a
// citation record the answer could carry a [1] marker with nothing behind it, and
// the document would never appear under the message.
func TestDocumentInlineContextNumbersAndCitesAttachments(t *testing.T) {
	stub := &inlineStub{
		docs: map[string]rag.Document{
			"d1": {ID: "d1", Filename: "runbook.md", ThreadID: strPtr("t1")},
			"d2": {ID: "d2", Filename: "policy.md", ThreadID: strPtr("t1")},
		},
		texts: map[string]string{"d1": "Retention is 45 days.", "d2": "Rollback at p95 > 9s."},
	}
	s := &server{documents: stub}
	docIdx := newDocIndexer()

	block, _, citations := s.documentInlineContext(context.Background(), "u1",
		chat.Thread{ID: "t1"}, []string{"d1", "d2"}, docIdx)

	if !strings.Contains(block, "[1] runbook.md") || !strings.Contains(block, "[2] policy.md") {
		t.Errorf("attachments should be labeled with their markers:\n%s", block)
	}
	if len(citations) != 2 {
		t.Fatalf("citations = %d, want 2", len(citations))
	}
	if citations[0].Index != 1 || citations[1].Index != 2 {
		t.Errorf("citation indices = %d,%d, want 1,2", citations[0].Index, citations[1].Index)
	}
	if citations[0].Filename != "runbook.md" || !citations[0].Full {
		t.Errorf("citation[0] = %+v, want runbook.md marked full", citations[0])
	}
	// The web-source registry must continue after the attachments.
	if got := docIdx.count(); got != 2 {
		t.Errorf("docIdx.count() = %d, want 2 (the offset web sources start after)", got)
	}
}

// A document skipped because the budget is already spent must not consume a marker.
// If it did, the sequence would carry a hole — [2] labelled nothing, yet counted, so
// the web sources start at [3] and the model can cite a number nothing backs.
func TestDocumentInlineContextSkippedDocTakesNoNumber(t *testing.T) {
	stub := &inlineStub{
		docs: map[string]rag.Document{
			"d1": {ID: "d1", Filename: "huge.txt", ThreadID: strPtr("t1")},
			"d2": {ID: "d2", Filename: "starved.md", ThreadID: strPtr("t1")},
		},
		texts: map[string]string{
			// d1 alone consumes the whole budget, leaving d2 no room at all.
			"d1": strings.Repeat("x", inlineDocByteBudget*2),
			"d2": "Never makes it in.",
		},
	}
	s := &server{documents: stub}
	docIdx := newDocIndexer()

	block, _, citations := s.documentInlineContext(context.Background(), "u1",
		chat.Thread{ID: "t1"}, []string{"d1", "d2"}, docIdx)

	if strings.Contains(block, "starved.md") {
		t.Fatalf("d2 should not have fit in the budget:\n%s", block[:200])
	}
	if len(citations) != 1 || citations[0].Index != 1 {
		t.Fatalf("citations = %+v, want only d1 at [1]", citations)
	}
	if got := docIdx.count(); got != 1 {
		t.Errorf("docIdx.count() = %d, want 1 — the skipped document must not burn a marker", got)
	}
}
