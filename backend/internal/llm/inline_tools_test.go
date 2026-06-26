package llm

import "testing"

func TestIsMiMoModel_MatchesDeployVariants(t *testing.T) {
	matches := []string{"mimo", "MiMo", "  MiMo  ", "MiMo-7B", "mimo-vl", "MiMo-7B-RL"}
	for _, name := range matches {
		if !isMiMoModel(name) {
			t.Errorf("isMiMoModel(%q) = false, want true", name)
		}
	}
	nonMatches := []string{"", "gpt-4o", "claude-opus", "llama-3"}
	for _, name := range nonMatches {
		if isMiMoModel(name) {
			t.Errorf("isMiMoModel(%q) = true, want false", name)
		}
	}
}

func TestFirstInlineToolName(t *testing.T) {
	// Name not yet streamed: nothing to surface.
	if got := firstInlineToolName("Sure.<tool_call>\n"); got != "" {
		t.Fatalf("firstInlineToolName before name = %q, want empty", got)
	}
	// Partial function tag (no closing '>'): still nothing.
	if got := firstInlineToolName("<tool_call>\n<function=create_pdf_fi"); got != "" {
		t.Fatalf("firstInlineToolName on partial tag = %q, want empty", got)
	}
	// Full tag arrived: the name surfaces even while the argument is still streaming.
	if got := firstInlineToolName("<tool_call>\n<function=create_pdf_file>\n<parameter=content>partial"); got != "create_pdf_file" {
		t.Fatalf("firstInlineToolName = %q, want create_pdf_file", got)
	}
	// The <tool_invocation …/> variant surfaces its name attribute the same way, even
	// while the large arguments value is still streaming.
	if got := firstInlineToolName(`<tool_invocation name="generate_image" arguments={"prompt": "partial`); got != "generate_image" {
		t.Fatalf("firstInlineToolName (variant) = %q, want generate_image", got)
	}
}

func TestInlineToolCallID(t *testing.T) {
	if got := inlineToolCallID(0); got != "inline_call_1" {
		t.Fatalf("inlineToolCallID(0) = %q, want inline_call_1", got)
	}
	if got := inlineToolCallID(2); got != "inline_call_3" {
		t.Fatalf("inlineToolCallID(2) = %q, want inline_call_3", got)
	}
}

func TestParseInlineToolCalls_SingleMiMoCall(t *testing.T) {
	content := "<tool_call>\n<function=tavily__tavily_search>\n<parameter=q>colossus forbin project</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].Type != "function" {
		t.Fatalf("call type = %q, want function", calls[0].Type)
	}
	if calls[0].Function.Name != "tavily__tavily_search" {
		t.Fatalf("call name = %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"q":"colossus forbin project"}` {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
}

// Verbatim assistant content captured from thread EE7PeO1kdXk5kEGUKpBecQ, where
// MiMo emitted two adjacent inline tool calls instead of native tool_calls.
func TestParseInlineToolCalls_RealMiMoCapture(t *testing.T) {
	content := "<tool_call>\n<function=tavily__tavily_search>\n<parameter=max_results>8</parameter>\n<parameter=q>Colossus Forbin Project 1970 Eric Braeden Hans Gudegast casting production history visual effects matte paintings</parameter>\n</function>\n</tool_call>" +
		"<tool_call>\n<function=tavily__tavily_search>\n<parameter=max_results>8</parameter>\n<parameter=q>Colossus Forbin Project James Bridges screenplay production design John Lloyd budget Universal 1970</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 2 {
		t.Fatalf("calls = %#v, want 2", calls)
	}
	for i, call := range calls {
		if call.Function.Name != "tavily__tavily_search" {
			t.Fatalf("call[%d] name = %q", i, call.Function.Name)
		}
	}
	if calls[0].Function.Arguments != `{"max_results":"8","q":"Colossus Forbin Project 1970 Eric Braeden Hans Gudegast casting production history visual effects matte paintings"}` {
		t.Fatalf("call[0] arguments = %q", calls[0].Function.Arguments)
	}
	if calls[1].Function.Arguments != `{"max_results":"8","q":"Colossus Forbin Project James Bridges screenplay production design John Lloyd budget Universal 1970"}` {
		t.Fatalf("call[1] arguments = %q", calls[1].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
}

func TestParseInlineToolCalls_MultipleCallsPreserveParameterOrder(t *testing.T) {
	content := "<tool_call>\n<function=tavily__tavily_search>\n<parameter=max_results>8</parameter>\n<parameter=q>colossus 1970 production</parameter>\n</function>\n</tool_call>\n" +
		"<tool_call>\n<function=tavily__tavily_search>\n<parameter=max_results>8</parameter>\n<parameter=q>forbin project budget</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 2 {
		t.Fatalf("calls = %#v, want 2", calls)
	}
	if calls[0].ID != "inline_call_1" || calls[1].ID != "inline_call_2" {
		t.Fatalf("call IDs = %q, %q", calls[0].ID, calls[1].ID)
	}
	if calls[0].Function.Arguments != `{"max_results":"8","q":"colossus 1970 production"}` {
		t.Fatalf("call[0] arguments = %q", calls[0].Function.Arguments)
	}
	if calls[1].Function.Arguments != `{"max_results":"8","q":"forbin project budget"}` {
		t.Fatalf("call[1] arguments = %q", calls[1].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
}

func TestParseInlineToolCalls_KeepsSurroundingProse(t *testing.T) {
	content := "Let me search for that.\n<tool_call>\n<function=tavily__tavily_search>\n<parameter=q>colossus</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if cleaned != "Let me search for that." {
		t.Fatalf("cleaned = %q", cleaned)
	}
}

func TestParseInlineToolCalls_NoToolCallReturnsContentUnchanged(t *testing.T) {
	content := "Colossus: The Forbin Project is a 1970 film."

	calls, cleaned := parseInlineToolCalls(content)

	if calls != nil {
		t.Fatalf("calls = %#v, want nil", calls)
	}
	if cleaned != content {
		t.Fatalf("cleaned = %q, want unchanged", cleaned)
	}
}

// Verbatim assistant content from the logo-redesign thread, where MiMo invented a
// third tool-call syntax (<tool_invocation name=… arguments={…} />) that no prompt
// teaches, leaking the raw markup to the UI instead of generating an image.
func TestParseInlineToolCalls_ToolInvocationVariant(t *testing.T) {
	content := `<tool_invocation name="generate_image" arguments={"filename": "rethinked-logo", "height": 1024, "prompt": "A modern, minimalist logo on a solid black background, icon in burnt orange (#c15f3c)."} />`

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].ID != "inline_call_1" {
		t.Fatalf("call id = %q, want inline_call_1", calls[0].ID)
	}
	if calls[0].Type != "function" {
		t.Fatalf("call type = %q, want function", calls[0].Type)
	}
	if calls[0].Function.Name != "generate_image" {
		t.Fatalf("call name = %q", calls[0].Function.Name)
	}
	// The arguments value is already valid JSON and is passed through verbatim.
	wantArgs := `{"filename": "rethinked-logo", "height": 1024, "prompt": "A modern, minimalist logo on a solid black background, icon in burnt orange (#c15f3c)."}`
	if calls[0].Function.Arguments != wantArgs {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
}

func TestParseInlineToolCalls_ToolInvocationKeepsSurroundingProse(t *testing.T) {
	content := `Sure, here it is. <tool_invocation name="generate_image" arguments={"prompt": "a red fox"} /> Done.`

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 || calls[0].Function.Name != "generate_image" {
		t.Fatalf("calls = %#v, want one generate_image call", calls)
	}
	if calls[0].Function.Arguments != `{"prompt": "a red fox"}` {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
	if cleaned != "Sure, here it is.  Done." {
		t.Fatalf("cleaned = %q", cleaned)
	}
}

// A JSON value containing braces and a '>' must not end the argument scan or the tag
// scan early.
func TestParseInlineToolCalls_ToolInvocationNestedBracesAndAngle(t *testing.T) {
	content := `<tool_invocation name="generate_image" arguments={"prompt": "a sign reading {SALE} 50% off >> here", "meta": {"w": 1024}} />`

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].Function.Arguments != `{"prompt": "a sign reading {SALE} 50% off >> here", "meta": {"w": 1024}}` {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
}

// Malformed/unbalanced arguments degrade gracefully: no panic, no parsed call, and
// the markup is left in place so finishStream's warning can surface it.
func TestParseInlineToolCalls_ToolInvocationUnbalancedArgsLeftForWarning(t *testing.T) {
	content := `<tool_invocation name="generate_image" arguments={"prompt": "truncated`

	calls, cleaned := parseInlineToolCalls(content)

	if calls != nil {
		t.Fatalf("calls = %#v, want nil", calls)
	}
	if cleaned != content {
		t.Fatalf("cleaned = %q, want unchanged", cleaned)
	}
}

func TestParseInlineToolCalls_EscapesSpecialCharactersInArguments(t *testing.T) {
	content := `<tool_call><function=tavily__tavily_search><parameter=q>"quoted" & line
break</parameter></function></tool_call>`

	calls, _ := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].Function.Arguments != `{"q":"\"quoted\" & line\nbreak"}` {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
}
