package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/imagegen"
)

func TestFallbackImageToolCallBuildsAGenerateImageCall(t *testing.T) {
	call, ok := fallbackImageToolCall("  Draw Darth Vader grocery shopping in the local deli  ")
	if !ok {
		t.Fatal("fallbackImageToolCall() reported no call for a non-empty prompt")
	}
	if call.Function.Name != "generate_image" {
		t.Fatalf("tool name = %q, want generate_image", call.Function.Name)
	}
	var args struct {
		Prompt   string `json:"prompt"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args.Prompt != "Draw Darth Vader grocery shopping in the local deli" {
		t.Fatalf("prompt = %q, want the trimmed user text", args.Prompt)
	}
	if args.Filename != "draw-darth-vader-grocery" {
		t.Fatalf("filename = %q, want one derived from the prompt", args.Filename)
	}
}

func TestFallbackImageToolCallReportsNoCallWithoutUserText(t *testing.T) {
	// Nothing to send: the caller keeps its own empty-handed return rather than
	// asking the provider to generate from an empty prompt.
	for _, prompt := range []string{"", "   \n\t "} {
		if _, ok := fallbackImageToolCall(prompt); ok {
			t.Fatalf("fallbackImageToolCall(%q) reported a call, want none", prompt)
		}
	}
}

func TestFallbackImageToolCallTruncatesToThePromptCap(t *testing.T) {
	call, ok := fallbackImageToolCall(strings.Repeat("ä", imagegen.MaxPromptRunes+500))
	if !ok {
		t.Fatal("fallbackImageToolCall() reported no call for an over-long prompt")
	}
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if got := len([]rune(args.Prompt)); got != imagegen.MaxPromptRunes {
		t.Fatalf("prompt runes = %d, want %d", got, imagegen.MaxPromptRunes)
	}
}

func TestImageFilenameFromPromptSkipsShortWordsAndBoundsLength(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   string
	}{
		{"a cat on an old mat", "cat-old-mat"},
		{"!!! ???", ""},
		{"Grosse Städte bei Nacht", "grosse-städte-bei-nacht"},
	} {
		if got := imageFilenameFromPrompt(tc.prompt); got != tc.want {
			t.Fatalf("imageFilenameFromPrompt(%q) = %q, want %q", tc.prompt, got, tc.want)
		}
	}
	long := imageFilenameFromPrompt(strings.Repeat("verylongword ", 4))
	if len([]rune(long)) > fallbackFilenameMaxRunes {
		t.Fatalf("filename = %q, want at most %d runes", long, fallbackFilenameMaxRunes)
	}
}
