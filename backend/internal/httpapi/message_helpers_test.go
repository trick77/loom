package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
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

// A pinned profile language is used by default but must be phrased so an explicit
// in-message request wins, and it must never force English on an unset profile.
func TestSystemPromptForUser_LanguageDirective(t *testing.T) {
	now := time.Now()

	for _, build := range []struct {
		name string
		fn   func(auth.User, time.Time) string
	}{
		{"chat", systemPromptForUser},
		{"incognito", incognitoSystemPromptForUser},
	} {
		// Pinned de -> escape-clause pin, no absolute "Always answer".
		pinned := build.fn(auth.User{ResponseLanguage: "de"}, now)
		if !strings.Contains(pinned, "Answer in German. If the user asks for a different language, or writes their message in a different language, reply in that language instead.") {
			t.Errorf("%s pinned de: missing escape-clause directive: %q", build.name, pinned)
		}
		if strings.Contains(pinned, "Always answer") {
			t.Errorf("%s pinned de: unexpected absolute directive: %q", build.name, pinned)
		}

		// Unset -> track the user's own language, never force English.
		unset := build.fn(auth.User{ResponseLanguage: ""}, now)
		if !strings.Contains(unset, "Answer in the language the user writes in.") {
			t.Errorf("%s unset: missing neutral directive: %q", build.name, unset)
		}
		if strings.Contains(unset, "English") {
			t.Errorf("%s unset: must not force English: %q", build.name, unset)
		}

		// A legacy "auto" row (predating removal) is treated as unset, not English.
		legacy := build.fn(auth.User{ResponseLanguage: "auto"}, now)
		if !strings.Contains(legacy, "Answer in the language the user writes in.") {
			t.Errorf("%s legacy auto: want neutral directive: %q", build.name, legacy)
		}
	}
}

// Threads created from the user's prompt keep that prompt as their title until a
// generated one lands, and prompt-derived titles are capitalized on write now.
// Rows written before that (or ones the ASCII-only backfill could not touch)
// still hold the verbatim form, so both count as untitled — otherwise they read
// as a user-chosen title and never get a generated one. A rename that differs
// from the prompt in case alone is the user's, though, and must survive.
func TestShouldGenerateThreadTitle_PromptTitleCapitalizedOrVerbatim(t *testing.T) {
	const prompt = "why is the sky blue?"

	cases := map[string]bool{
		"Why is the sky blue?":   true,
		"why is the sky blue?":   true,
		"WHY IS THE SKY BLUE?":   false,
		"über die wolken":        false,
		"New thread":             true,
		"Blue Sky Explanation":   false,
		"A title the user chose": false,
	}
	for currentTitle, want := range cases {
		if got := shouldGenerateThreadTitle(currentTitle, prompt); got != want {
			t.Errorf("shouldGenerateThreadTitle(%q, %q) = %v, want %v", currentTitle, prompt, got, want)
		}
	}
}
