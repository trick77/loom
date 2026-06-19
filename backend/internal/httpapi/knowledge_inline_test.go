package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/rag"
)

func TestKnowledgeInlineContext_injectsWholeWhenUnderBudget(t *testing.T) {
	s := &server{
		documents: &inlineStub{
			indexed: []rag.IndexedDoc{
				{ID: "d1", Filename: "briefing.pdf", TokenCount: 100},
				{ID: "d2", Filename: "agenda.md", TokenCount: 50},
			},
			texts: map[string]string{
				"d1": "Slide 20: the preparation task is to review measures.",
				"d2": "09:00 kickoff, 16:30 feedback.",
			},
		},
		knowledgeInlineTokenBudget: 1000,
	}

	block, inlinedIDs, citations, inlinedAll := s.knowledgeInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, nil)

	if !strings.Contains(block, "briefing.pdf") || !strings.Contains(block, "the preparation task is to review measures.") {
		t.Errorf("block missing full document text: %q", block)
	}
	if !strings.Contains(block, "agenda.md") || !strings.Contains(block, "16:30 feedback.") {
		t.Errorf("block missing second document: %q", block)
	}
	if !inlinedIDs["d1"] || !inlinedIDs["d2"] {
		t.Errorf("both docs should be marked inlined (excluded from RAG): %v", inlinedIDs)
	}
	if len(citations) != 2 || citations[0].Score != 1.0 {
		t.Errorf("want 2 full-document citations with score 1.0, got %+v", citations)
	}
	if !inlinedAll {
		t.Error("inlinedAll should be true when the whole knowledge base fits the budget (RAG can be skipped)")
	}
}

func TestKnowledgeInlineContext_leavesOversizeForRAG(t *testing.T) {
	s := &server{
		documents: &inlineStub{
			indexed: []rag.IndexedDoc{
				{ID: "small", Filename: "small.md", TokenCount: 100},
				{ID: "big", Filename: "big.pdf", TokenCount: 5000},
			},
			texts: map[string]string{"small": "fits", "big": "too large to inline"},
		},
		knowledgeInlineTokenBudget: 1000,
	}

	block, inlinedIDs, _, inlinedAll := s.knowledgeInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, nil)

	if !inlinedIDs["small"] {
		t.Error("the small doc should be inlined")
	}
	if inlinedIDs["big"] {
		t.Error("the oversize doc must not be inlined")
	}
	if strings.Contains(block, "too large to inline") {
		t.Errorf("oversize doc text leaked into block: %q", block)
	}
	if inlinedAll {
		t.Error("inlinedAll must be false when an in-scope doc was left for RAG")
	}
}

func TestKnowledgeInlineContext_disabledWhenBudgetZero(t *testing.T) {
	s := &server{
		documents: &inlineStub{
			indexed: []rag.IndexedDoc{{ID: "d1", Filename: "a.md", TokenCount: 10}},
			texts:   map[string]string{"d1": "hi"},
		},
		knowledgeInlineTokenBudget: 0,
	}
	block, inlinedIDs, citations, inlinedAll := s.knowledgeInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, nil)
	if block != "" || inlinedIDs != nil || citations != nil || inlinedAll {
		t.Errorf("budget 0 should disable inlining, got block=%q ids=%v inlinedAll=%v", block, inlinedIDs, inlinedAll)
	}
}

func TestKnowledgeInlineContext_skipsAlreadyAttachedDoc(t *testing.T) {
	s := &server{
		documents: &inlineStub{
			indexed: []rag.IndexedDoc{{ID: "d1", Filename: "a.md", TokenCount: 10}},
			texts:   map[string]string{"d1": "already attached in full"},
		},
		knowledgeInlineTokenBudget: 1000,
	}
	// d1 was already inlined this turn as an explicit attachment.
	block, inlinedIDs, _, inlinedAll := s.knowledgeInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, map[string]bool{"d1": true})
	if strings.Contains(block, "already attached in full") {
		t.Errorf("a doc inlined as an attachment must not be re-injected here: %q", block)
	}
	if len(inlinedIDs) != 0 {
		t.Errorf("nothing should be inlined here, got %v", inlinedIDs)
	}
	// The only in-scope doc is already covered by the attachment, so RAG can be skipped.
	if !inlinedAll {
		t.Error("inlinedAll should be true when every in-scope doc is covered by an attachment")
	}
}

func TestKnowledgeInlineContext_skipsDocWithNoExtractableText(t *testing.T) {
	s := &server{
		documents: &inlineStub{
			indexed: []rag.IndexedDoc{{ID: "d1", Filename: "empty.pdf", TokenCount: 100}},
			texts:   map[string]string{"d1": "   "}, // extracts to whitespace only
		},
		knowledgeInlineTokenBudget: 1000,
	}
	block, inlinedIDs, citations, inlinedAll := s.knowledgeInlineContext(context.Background(), "u1", chat.Thread{ID: "t1"}, nil)
	if block != "" || len(inlinedIDs) != 0 || citations != nil {
		t.Errorf("a doc with no extractable text must not be inlined, got block=%q ids=%v", block, inlinedIDs)
	}
	// It was neither inlined nor attached, so it stays RAG-eligible.
	if inlinedAll {
		t.Error("inlinedAll must be false when an in-scope doc could not be inlined")
	}
}
