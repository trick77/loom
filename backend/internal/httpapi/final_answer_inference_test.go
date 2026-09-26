package httpapi

import (
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// finalAnswerInference must widen the completion budget on top of the normal
// purpose/round metadata, so a synthesis over many sources has room to finish
// after the model's reasoning.
func TestFinalAnswerInferenceWidensBudget(t *testing.T) {
	base := llm.InferenceMetadata{ThreadID: "thr_1"}

	got := finalAnswerInference(base, "chat_final", maxToolRounds+1)

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
// gets the wider budget; any other empty turn retries unchanged.
func TestIncognitoRetryInferenceWidensOnlyAfterCapHit(t *testing.T) {
	base := llm.InferenceMetadata{ThreadID: "thr_1"}

	capped := incognitoRetryInference(base, llm.StreamResult{FinishReason: "length"})
	if capped.MaxCompletionTokens != finalAnswerMaxCompletionTokens {
		t.Fatalf("after cap hit: MaxCompletionTokens=%d, want %d", capped.MaxCompletionTokens, finalAnswerMaxCompletionTokens)
	}
	plain := incognitoRetryInference(base, llm.StreamResult{FinishReason: "stop"})
	if plain.MaxCompletionTokens != 0 {
		t.Fatalf("after stop: MaxCompletionTokens=%d, want 0", plain.MaxCompletionTokens)
	}
	if capped.Purpose != "chat" || capped.Round != 2 || plain.Purpose != "chat" || plain.Round != 2 {
		t.Fatalf("purpose/round = %q/%d and %q/%d, want chat/2", capped.Purpose, capped.Round, plain.Purpose, plain.Round)
	}
}
