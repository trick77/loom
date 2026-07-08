package httpapi

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/trick77/loom/internal/llm"
)

func TestCollectToolNotes_joinsToolMessagesOnly(t *testing.T) {
	rounds := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},
		{Role: "tool", ToolCallID: "c1", Content: "first note"},
		{Role: "assistant", Content: "  "},
		{Role: "tool", ToolCallID: "c2", Content: "second note"},
		{Role: "tool", ToolCallID: "c3", Content: "   "}, // blank, skipped
	}

	got := collectToolNotes(rounds, 0)

	if got != "first note\n\nsecond note" {
		t.Fatalf("collectToolNotes = %q, want the two tool notes joined", got)
	}
}

func TestCollectToolNotes_capKeepsTailAndValidUTF8(t *testing.T) {
	// A note full of multi-byte runes (ü = 2 bytes), longer than the cap, so the
	// tail cut is forced to land inside a rune.
	big := strings.Repeat("üüüü", 500) // 4000 bytes
	rounds := []llm.Message{{Role: "tool", ToolCallID: "c1", Content: big}}
	const cap = 101 // odd byte cap → cut cannot align to the 2-byte rune boundary

	got := collectToolNotes(rounds, cap)

	if len(got) > cap {
		t.Fatalf("len(got) = %d, want <= cap %d", len(got), cap)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("collectToolNotes produced invalid UTF-8 at a mid-rune cut: %q", got)
	}
	// Tail is kept: the result is a suffix of the (valid) source once the partial
	// leading rune is dropped.
	if !strings.HasSuffix(big, got) {
		t.Fatalf("result is not a tail slice of the source: %q", got)
	}
}
