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

func TestCollectToolNotes_capKeepsHeadAndValidUTF8(t *testing.T) {
	// A note full of multi-byte runes (ü = 2 bytes), longer than the cap, so the
	// cut is forced to land inside a rune.
	big := strings.Repeat("üüüü", 500) // 4000 bytes
	rounds := []llm.Message{{Role: "tool", ToolCallID: "c1", Content: big}}
	const cap = 101 // odd byte cap → cut cannot align to the 2-byte rune boundary

	got := collectToolNotes(rounds, cap)

	if !utf8.ValidString(got) {
		t.Fatalf("collectToolNotes produced invalid UTF-8 at a mid-rune cut: %q", got)
	}
	// The head is kept now, not the tail: a web note leads with its
	// "Web source [n]: url" header, and losing that strands the citation marker.
	if !strings.HasPrefix(big, strings.TrimSuffix(got, truncationMarker)) {
		t.Fatalf("result is not a head slice of the source: %q", got)
	}
}

// Every gathered source must survive the budget. The old tail-only cut dropped
// whole early notes, so the model was asked to cite [1]..[n] while it could only
// see the last few — the main cause of answers that cite nothing.
func TestCollectToolNotes_overBudgetKeepsEverySourceHeader(t *testing.T) {
	var rounds []llm.Message
	for i := 1; i <= 6; i++ {
		rounds = append(rounds, llm.Message{
			Role:       "tool",
			ToolCallID: "c",
			Content: "Web source [" + itoa(i) + "]: https://example.com/" + itoa(i) +
				"\n\n" + strings.Repeat("filler ", 400),
		})
	}
	// Well under the total, forcing per-note truncation.
	got := collectToolNotes(rounds, 1200)

	for i := 1; i <= 6; i++ {
		marker := "Web source [" + itoa(i) + "]"
		if !strings.Contains(got, marker) {
			t.Errorf("note %d was dropped entirely; %q missing from the folded notes", i, marker)
		}
	}
	// Budget plus only the "\n\n" separators between the six notes.
	if len(got) > 1200+2*5 {
		t.Errorf("len(got) = %d, over the 1200-byte budget", len(got))
	}
}

// A note under its fair share must not be padded or cut, and its surplus should
// let a larger sibling keep more than a flat 1/n slice.
func TestCollectToolNotes_smallNotesSurviveIntactAndDonateSurplus(t *testing.T) {
	small := "Web source [1]: https://a.example\n\nshort"
	big := "Web source [2]: https://b.example\n\n" + strings.Repeat("x", 2000)
	rounds := []llm.Message{
		{Role: "tool", ToolCallID: "c1", Content: small},
		{Role: "tool", ToolCallID: "c2", Content: big},
	}

	got := collectToolNotes(rounds, 1000)

	if !strings.Contains(got, small) {
		t.Errorf("the small note was truncated despite fitting its share: %q", got)
	}
	// Flat share would be 500; the small note donates its unused ~460 bytes.
	bigPart := got[strings.Index(got, "Web source [2]"):]
	if len(bigPart) <= 520 {
		t.Errorf("big note kept %d bytes, want > 520 (surplus from the small note was not donated)", len(bigPart))
	}
}

func TestWebSourceIndexBlock_listsEverySourceWithMarkerAndURL(t *testing.T) {
	sources := []webSource{
		{Index: 1, URL: "https://truefoundry.com/blog", Label: "Truefoundry"},
		{Index: 2, URL: "https://kubernetes.io/docs", Label: "Kubernetes"},
	}

	got := webSourceIndexBlock(sources)

	for _, want := range []string{"[1]", "Truefoundry", "https://truefoundry.com/blog", "[2]", "Kubernetes"} {
		if !strings.Contains(got, want) {
			t.Errorf("index block missing %q:\n%s", want, got)
		}
	}
	if webSourceIndexBlock(nil) != "" {
		t.Error("no sources should yield an empty block, not a stray header")
	}
}

func TestBuildFinalSynthesisHistory_carriesCitationRuleAndSourceIndex(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},
		{Role: "tool", ToolCallID: "c1", Content: "Web source [1]: https://a.example\n\nnote body"},
	}
	sources := []webSource{{Index: 1, URL: "https://a.example", Label: "A"}}

	synth, ok := buildFinalSynthesisHistory(history, 2, "", sources)
	if !ok {
		t.Fatal("buildFinalSynthesisHistory returned ok=false with notes present")
	}
	folded := synth[len(synth)-1].Content

	// The citation rule lives only in the system prompt, far up the history; the
	// forced final turn must restate it next to the notes it governs.
	if !strings.Contains(folded, "[1]") {
		t.Errorf("folded turn does not mention the [n] marker form:\n%s", folded)
	}
	if !strings.Contains(strings.ToLower(folded), "cite") {
		t.Errorf("folded turn carries no citation instruction:\n%s", folded)
	}
	// The full source index must be present even though the notes fit here, so a
	// later truncation can never strand the marker->identity mapping.
	if !strings.Contains(folded, "https://a.example") {
		t.Errorf("folded turn is missing the source index:\n%s", folded)
	}
	if !strings.Contains(folded, "note body") {
		t.Errorf("folded turn lost the research notes:\n%s", folded)
	}
}

func itoa(i int) string { return string(rune('0' + i)) }
