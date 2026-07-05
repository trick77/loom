package httpapi

import (
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// normalizeReasoningEffort passes MiMo's accepted values through unchanged and
// clamps everything else (empty, unknown, an old client that never sends one) to
// the default, so a bad value can never reach the outbound reasoning_effort field.
func TestNormalizeReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"":        llm.DefaultReasoningEffort,
		"xhigh":   llm.DefaultReasoningEffort,
		"HIGH":    llm.DefaultReasoningEffort,
		"maximum": llm.DefaultReasoningEffort,
	}
	for input, want := range cases {
		if got := normalizeReasoningEffort(input); got != want {
			t.Errorf("normalizeReasoningEffort(%q) = %q, want %q", input, got, want)
		}
	}
}
