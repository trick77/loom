package httpapi

import (
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// finalAnswerInference must disable thinking and widen the completion budget on
// top of the normal purpose/round metadata, so the forced final answer writes
// prose instead of burning the whole budget on reasoning.
func TestFinalAnswerInferenceSuppressesThinkingAndWidensBudget(t *testing.T) {
	base := llm.InferenceMetadata{ReasoningEffort: "high", ThreadID: "thr_1"}

	got := finalAnswerInference(base, "chat_final", maxToolRounds+1)

	if !got.SuppressThinking {
		t.Fatalf("SuppressThinking = false, want true")
	}
	if got.MaxCompletionTokens != finalAnswerMaxCompletionTokens {
		t.Fatalf("MaxCompletionTokens = %d, want %d", got.MaxCompletionTokens, finalAnswerMaxCompletionTokens)
	}
	if got.Purpose != "chat_final" || got.Round != maxToolRounds+1 {
		t.Fatalf("purpose/round = %q/%d, want chat_final/%d", got.Purpose, got.Round, maxToolRounds+1)
	}
	// Unrelated fields carry through unchanged.
	if got.ThreadID != "thr_1" {
		t.Fatalf("ThreadID = %q, want thr_1", got.ThreadID)
	}
}
