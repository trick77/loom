package rag

import (
	"strings"
	"testing"
)

func TestChunk_emptyTextYieldsNoChunks(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t  \n"} {
		if got := Chunk(in, DefaultChunkOptions()); len(got) != 0 {
			t.Errorf("Chunk(%q) = %d chunks, want 0", in, len(got))
		}
	}
}

func TestChunk_shortTextIsSingleChunk(t *testing.T) {
	in := "The quick brown fox jumps over the lazy dog."
	got := Chunk(in, DefaultChunkOptions())
	if len(got) != 1 {
		t.Fatalf("Chunk() = %d chunks, want 1", len(got))
	}
	if got[0].Ordinal != 0 {
		t.Errorf("ordinal = %d, want 0", got[0].Ordinal)
	}
	if strings.TrimSpace(got[0].Text) != in {
		t.Errorf("text = %q, want %q", got[0].Text, in)
	}
	if got[0].TokenCount <= 0 {
		t.Errorf("token count = %d, want > 0", got[0].TokenCount)
	}
}

func TestChunk_longTextSplitsWithOverlapAndOrder(t *testing.T) {
	// ~4000 words => well beyond a single chunk budget.
	words := make([]string, 4000)
	for i := range words {
		words[i] = "word"
	}
	in := strings.Join(words, " ")

	opts := DefaultChunkOptions()
	got := Chunk(in, opts)
	if len(got) < 2 {
		t.Fatalf("Chunk() = %d chunks, want >= 2 for long input", len(got))
	}

	for i, c := range got {
		if c.Ordinal != i {
			t.Errorf("chunk[%d] ordinal = %d, want %d", i, c.Ordinal, i)
		}
		if c.TokenCount > opts.MaxTokens {
			t.Errorf("chunk[%d] token count = %d, exceeds MaxTokens %d", i, c.TokenCount, opts.MaxTokens)
		}
		if strings.TrimSpace(c.Text) == "" {
			t.Errorf("chunk[%d] is empty", i)
		}
	}
}

func TestChunk_overlapSharesTrailingContent(t *testing.T) {
	// Distinct numbered words so we can detect shared content between neighbours.
	parts := make([]string, 2000)
	for i := range parts {
		parts[i] = "w" + itoa(i)
	}
	in := strings.Join(parts, " ")

	opts := DefaultChunkOptions()
	got := Chunk(in, opts)
	if len(got) < 2 {
		t.Fatalf("need >= 2 chunks to test overlap, got %d", len(got))
	}

	firstWords := strings.Fields(got[0].Text)
	secondWords := strings.Fields(got[1].Text)
	tail := firstWords[len(firstWords)-1]
	if !contains(secondWords, tail) {
		t.Errorf("chunk[1] does not share trailing content %q from chunk[0]; overlap missing", tail)
	}
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
