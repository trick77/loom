package llm

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

// userMemoryBudgetChars is mirrored from chat.MaxUserMemoryLength because llm
// deliberately does not import chat in non-test code. This test fails loudly if
// the two ever drift apart.
func TestUserMemoryBudgetMatchesStoreCap(t *testing.T) {
	if userMemoryBudgetChars != chat.MaxUserMemoryLength {
		t.Fatalf("userMemoryBudgetChars (%d) != chat.MaxUserMemoryLength (%d); keep them in lockstep", userMemoryBudgetChars, chat.MaxUserMemoryLength)
	}
}

func TestUserMemorySectionsSumTo100(t *testing.T) {
	total := 0
	for _, s := range userMemorySections {
		total += s.pct
	}
	if total != 100 {
		t.Fatalf("userMemorySections pct sum = %d, want 100", total)
	}
}

func TestUserMemorySystemPromptIncludesNewHeadings(t *testing.T) {
	for _, want := range []string{"## Work context", "## Personal context", "## Top of mind", "## Brief history", "### Recent months", "### Earlier context", "### Long-term background"} {
		if !strings.Contains(UserMemorySystemPrompt, want) {
			t.Errorf("prompt missing new heading %q", want)
		}
	}
	// The computed per-section char target for Recent months (the largest, 36%)
	// must appear, proving the budget is divided in code rather than hardcoded.
	if !strings.Contains(UserMemorySystemPrompt, "2160 characters") {
		t.Errorf("prompt missing computed Recent months target (2160 = 6000*36/100)")
	}
}
