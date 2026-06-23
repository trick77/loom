package httpapi

import (
	"testing"
	"time"

	"github.com/trick77/loom/internal/llm"
)

// The context-window percentage must be driven by the final answer call's own
// model-reported total_tokens, NOT the per-turn accumulated usage (which sums the
// prompt across every tool round and helper call and so overstates occupancy).
// This pins that boundary: ContextTokens comes from result.Usage while the other
// token fields come from the accumulated usage.
func TestMessageMetricsFromTurn_ContextTokensFromFinalCallNotAccumulator(t *testing.T) {
	result := llm.StreamResult{
		Model: "mimo-v2.5-pro",
		Usage: llm.TokenUsage{PromptTokens: 48000, CompletionTokens: 1500, TotalTokens: 49500},
	}
	// Accumulated across the whole turn (deliberately much larger than the single
	// final call): 6 tool rounds re-counting the growing prompt + helper calls.
	accumulated := llm.TokenUsage{PromptTokens: 240000, CompletionTokens: 9000, TotalTokens: 249000}

	metrics := messageMetricsFromTurn(result, accumulated, 5*time.Second)

	if metrics.ContextTokens == nil {
		t.Fatal("ContextTokens is nil; expected the final call's total_tokens")
	}
	if *metrics.ContextTokens != 49500 {
		t.Errorf("ContextTokens = %d, want 49500 (final call's total_tokens)", *metrics.ContextTokens)
	}
	// The accumulated figures must be untouched — only the percentage source changed.
	if metrics.TotalTokens == nil || *metrics.TotalTokens != 249000 {
		t.Errorf("TotalTokens = %v, want 249000 (accumulated)", metrics.TotalTokens)
	}
	if metrics.PromptTokens == nil || *metrics.PromptTokens != 240000 {
		t.Errorf("PromptTokens = %v, want 240000 (accumulated)", metrics.PromptTokens)
	}
	if metrics.CompletionTokens == nil || *metrics.CompletionTokens != 9000 {
		t.Errorf("CompletionTokens = %v, want 9000 (accumulated)", metrics.CompletionTokens)
	}
}

// A turn whose final call reported no usage (e.g. interrupted/stalled before the
// usage chunk) records no ContextTokens, so the UI hides the percentage rather
// than showing a wrong one.
func TestMessageMetricsFromTurn_NoContextTokensWhenFinalCallUsageAbsent(t *testing.T) {
	result := llm.StreamResult{Model: "mimo-v2.5-pro"} // zero Usage
	accumulated := llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}

	metrics := messageMetricsFromTurn(result, accumulated, time.Second)

	if metrics.ContextTokens != nil {
		t.Errorf("ContextTokens = %v, want nil when the final call reported no usage", *metrics.ContextTokens)
	}
}
