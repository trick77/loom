package httpapi

import (
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// finalAnswerInference must disable thinking and widen the completion budget on
// top of the normal purpose/round metadata, so the forced final answer writes
// prose instead of burning the whole budget on reasoning.
func TestFinalAnswerInferenceSuppressesThinkingAndWidensBudget(t *testing.T) {
	base := llm.InferenceMetadata{ThreadID: "thr_1"}

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

// An incognito first turn that hit the cap spent it on reasoning, so its retry
// must run with thinking off; any other empty turn retries unchanged.
func TestIncognitoRetryInferenceSuppressesThinkingOnlyAfterCapHit(t *testing.T) {
	base := llm.InferenceMetadata{ThreadID: "thr_1"}

	capped := incognitoRetryInference(base, llm.StreamResult{FinishReason: "length"})
	if !capped.SuppressThinking || capped.MaxCompletionTokens != finalAnswerMaxCompletionTokens {
		t.Fatalf("after cap hit: SuppressThinking=%v MaxCompletionTokens=%d, want true/%d", capped.SuppressThinking, capped.MaxCompletionTokens, finalAnswerMaxCompletionTokens)
	}
	plain := incognitoRetryInference(base, llm.StreamResult{FinishReason: "stop"})
	if plain.SuppressThinking || plain.MaxCompletionTokens != 0 {
		t.Fatalf("after stop: SuppressThinking=%v MaxCompletionTokens=%d, want false/0", plain.SuppressThinking, plain.MaxCompletionTokens)
	}
	if capped.Purpose != "chat" || capped.Round != 2 || plain.Purpose != "chat" || plain.Round != 2 {
		t.Fatalf("purpose/round = %q/%d and %q/%d, want chat/2", capped.Purpose, capped.Round, plain.Purpose, plain.Round)
	}
}
