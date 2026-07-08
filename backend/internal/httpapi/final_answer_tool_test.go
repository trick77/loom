package httpapi

import (
	"testing"

	"github.com/trick77/loom/internal/llm"
)

func TestParseFinalAnswerText(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"plain", `{"text":"the answer"}`, "the answer"},
		{"trims whitespace", `{"text":"  padded  "}`, "padded"},
		{"missing text key", `{"other":"x"}`, ""},
		{"empty text", `{"text":"   "}`, ""},
		{"truncated json", `{"text":"cut off in the mi`, ""},
		{"not an object", `null`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseFinalAnswerText(tc.args); got != tc.want {
				t.Fatalf("parseFinalAnswerText(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestAdoptFinalAnswer_liftsToolText(t *testing.T) {
	in := llm.StreamResult{ToolCalls: []llm.ToolCall{
		{ID: "call_final", Function: llm.ToolCallFunction{Name: finalAnswerToolName, Arguments: `{"text":"synthesized"}`}},
	}}

	out := adoptFinalAnswer(in)

	if out.Content != "synthesized" {
		t.Fatalf("Content = %q, want the lifted text", out.Content)
	}
	if len(out.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %#v, want the adopted call consumed", out.ToolCalls)
	}
}

func TestAdoptFinalAnswer_prefersExistingProse(t *testing.T) {
	in := llm.StreamResult{
		Content: "real prose",
		ToolCalls: []llm.ToolCall{
			{ID: "call_final", Function: llm.ToolCallFunction{Name: finalAnswerToolName, Arguments: `{"text":"ignored"}`}},
		},
	}

	out := adoptFinalAnswer(in)

	if out.Content != "real prose" {
		t.Fatalf("Content = %q, want the direct prose to win", out.Content)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want untouched when prose already present", out.ToolCalls)
	}
}

func TestAdoptFinalAnswer_leavesOtherToolsAndEmptyContent(t *testing.T) {
	in := llm.StreamResult{ToolCalls: []llm.ToolCall{
		{ID: "call_fetch", Function: llm.ToolCallFunction{Name: "fetch__fetch", Arguments: `{"url":"x"}`}},
	}}

	out := adoptFinalAnswer(in)

	if out.Content != "" {
		t.Fatalf("Content = %q, want empty so the caller retries", out.Content)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want the non-final call left in place", out.ToolCalls)
	}
}

func TestAdoptFinalAnswer_blanksRawMarkupContent(t *testing.T) {
	// A final_answer call truncated at the token cap can leave an unclosed
	// <tool_call> block that the inline parser cannot recover; if upstream scrubbing
	// were bypassed the raw markup would sit in Content. adoptFinalAnswer must never
	// surface it — it blanks the content so the caller retries / falls back.
	in := llm.StreamResult{
		Content: `<tool_call><function=final_answer><parameter=text>partial answer cut off`,
	}

	out := adoptFinalAnswer(in)

	if out.Content != "" {
		t.Fatalf("Content = %q, want blanked so no raw markup leaks", out.Content)
	}
}

func TestFinalAnswerTool_schema(t *testing.T) {
	tool := finalAnswerTool()
	if tool.Function.Name != finalAnswerToolName {
		t.Fatalf("name = %q, want %q", tool.Function.Name, finalAnswerToolName)
	}
	required, ok := tool.Function.Parameters["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "text" {
		t.Fatalf("required = %#v, want [text]", tool.Function.Parameters["required"])
	}
}
