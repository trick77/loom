package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
)

func TestFallbackImageToolCallBuildsAGenerateImageCall(t *testing.T) {
	call, ok := fallbackImageToolCall("  Draw Darth Vader grocery shopping in the local deli  ")
	if !ok {
		t.Fatal("fallbackImageToolCall() reported no call for a non-empty prompt")
	}
	if call.Function.Name != "generate_image" {
		t.Fatalf("tool name = %q, want generate_image", call.Function.Name)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args["prompt"] != "Draw Darth Vader grocery shopping in the local deli" {
		t.Fatalf("prompt = %q, want the trimmed user text", args["prompt"])
	}
	// The provider derives the filename from the prompt; sending one here would
	// bypass that and reach the artifact store with characters it cannot keep.
	if _, ok := args["filename"]; ok {
		t.Fatalf("arguments carry a filename: %v", args)
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

func TestKeepInterruptedKeepsEarlierRoundsProse(t *testing.T) {
	b := &blockBuilder{}
	b.addText("Searching first.")
	result := llm.StreamResult{}
	if !b.keepInterrupted(&result, context.Canceled, nil) {
		t.Fatal("keepInterrupted dropped a turn whose earlier round streamed prose")
	}
	if result.Content != "Searching first." {
		t.Fatalf("content = %q, want the earlier round's prose", result.Content)
	}
	if b.keepInterrupted(&llm.StreamResult{}, errors.New("boom"), nil) {
		t.Fatal("keepInterrupted kept a turn that failed rather than being interrupted")
	}
	// A stall is the upstream's failure, not the user's stop: it still has to
	// reach the user as an error unless the stalled round itself streamed text.
	stalled := fmt.Errorf("read: %w", llm.ErrStreamStalled)
	if b.keepInterrupted(&llm.StreamResult{}, stalled, nil) {
		t.Fatal("keepInterrupted turned a stalled round into a finished answer")
	}
}

func TestPersistInterruptedPartial(t *testing.T) {
	stalled := fmt.Errorf("read chat completion stream: %w", llm.ErrStreamStalled)
	cases := []struct {
		name   string
		result llm.StreamResult
		err    error
		want   bool
	}{
		{"client cancel with content", llm.StreamResult{Content: "partial"}, context.Canceled, true},
		{"stall with content", llm.StreamResult{Content: "partial"}, stalled, true},
		{"stall reasoning only", llm.StreamResult{ReasoningContent: "thought"}, stalled, false},
		{"cancel no content", llm.StreamResult{}, context.Canceled, false},
		{"unrelated error with content", llm.StreamResult{Content: "partial"}, errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := persistInterruptedPartial(tc.result, tc.err); got != tc.want {
				t.Fatalf("persistInterruptedPartial = %v, want %v", got, tc.want)
			}
		})
	}
}
