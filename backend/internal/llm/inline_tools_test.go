package llm

import "testing"

func TestParseInlineToolCalls_SingleMiMoCall(t *testing.T) {
	content := "<tool_call>\n<function=searxng__web_search>\n<parameter=q>colossus forbin project</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].Type != "function" {
		t.Fatalf("call type = %q, want function", calls[0].Type)
	}
	if calls[0].Function.Name != "searxng__web_search" {
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
	content := "<tool_call>\n<function=searxng__web_search>\n<parameter=max_results>8</parameter>\n<parameter=q>Colossus Forbin Project 1970 Eric Braeden Hans Gudegast casting production history visual effects matte paintings</parameter>\n</function>\n</tool_call>" +
		"<tool_call>\n<function=searxng__web_search>\n<parameter=max_results>8</parameter>\n<parameter=q>Colossus Forbin Project James Bridges screenplay production design John Lloyd budget Universal 1970</parameter>\n</function>\n</tool_call>"

	calls, cleaned := parseInlineToolCalls(content)

	if len(calls) != 2 {
		t.Fatalf("calls = %#v, want 2", calls)
	}
	for i, call := range calls {
		if call.Function.Name != "searxng__web_search" {
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
	content := "<tool_call>\n<function=searxng__web_search>\n<parameter=max_results>8</parameter>\n<parameter=q>colossus 1970 production</parameter>\n</function>\n</tool_call>\n" +
		"<tool_call>\n<function=searxng__web_search>\n<parameter=max_results>8</parameter>\n<parameter=q>forbin project budget</parameter>\n</function>\n</tool_call>"

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
	content := "Let me search for that.\n<tool_call>\n<function=searxng__web_search>\n<parameter=q>colossus</parameter>\n</function>\n</tool_call>"

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

func TestParseInlineToolCalls_EscapesSpecialCharactersInArguments(t *testing.T) {
	content := `<tool_call><function=searxng__web_search><parameter=q>"quoted" & line
break</parameter></function></tool_call>`

	calls, _ := parseInlineToolCalls(content)

	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want 1", calls)
	}
	if calls[0].Function.Arguments != `{"q":"\"quoted\" & line\nbreak"}` {
		t.Fatalf("call arguments = %q", calls[0].Function.Arguments)
	}
}
