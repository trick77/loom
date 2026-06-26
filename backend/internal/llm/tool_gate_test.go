package llm

import "testing"

// collect feeds deltas through the gate and returns everything it allowed to
// stream, including the final flush.
func collect(g *toolCallStreamGate, deltas ...string) string {
	var out string
	for _, d := range deltas {
		out += g.push(d)
	}
	return out + g.flush()
}

func TestToolCallStreamGate_SuppressesToolCallAtStart(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g, "<tool_call>", "<function=tavily__tavily_search>", "</function></tool_call>")

	if emitted != "" {
		t.Fatalf("emitted = %q, want empty (tool call suppressed)", emitted)
	}
}

func TestToolCallStreamGate_StreamsProseBeforeToolCall(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g, "Let me search.\n", "<tool_call><function=x></function></tool_call>")

	if emitted != "Let me search.\n" {
		t.Fatalf("emitted = %q, want prose only", emitted)
	}
}

func TestToolCallStreamGate_SuppressesMarkerSplitAcrossDeltas(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g, "<too", "l_ca", "ll><function=x></function></tool_call>")

	if emitted != "" {
		t.Fatalf("emitted = %q, want empty (split marker still suppressed)", emitted)
	}
}

func TestToolCallStreamGate_SuppressesToolInvocationVariant(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g,
		"Sure. ",
		`<tool_invocation name="generate_image" `,
		`arguments={"prompt": "a red fox"} />`,
	)

	if emitted != "Sure. " {
		t.Fatalf("emitted = %q, want prose only (invocation suppressed)", emitted)
	}
}

func TestToolCallStreamGate_SuppressesToolInvocationSplitAcrossDeltas(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g, "<tool_inv", "ocation name=\"x\" arguments={} />")

	if emitted != "" {
		t.Fatalf("emitted = %q, want empty (split invocation marker still suppressed)", emitted)
	}
}

func TestCutAtFirstInlineMarker(t *testing.T) {
	// No marker: returned unchanged.
	if got := cutAtFirstInlineMarker("a normal answer with no markup"); got != "a normal answer with no markup" {
		t.Fatalf("cutAtFirstInlineMarker(no marker) = %q, want unchanged", got)
	}
	// Truncated <tool_invocation> markup is scrubbed, keeping the prose before it.
	if got := cutAtFirstInlineMarker(`Here you go. <tool_invocation name="generate_image" arguments={"prompt": "truncated`); got != "Here you go." {
		t.Fatalf("cutAtFirstInlineMarker(invocation) = %q, want %q", got, "Here you go.")
	}
	// Either marker is honored; the earliest wins.
	if got := cutAtFirstInlineMarker("prose <tool_call> then <tool_invocation"); got != "prose" {
		t.Fatalf("cutAtFirstInlineMarker(tool_call) = %q, want %q", got, "prose")
	}
}

func TestToolCallStreamGate_PassesNormalContentThrough(t *testing.T) {
	g := &toolCallStreamGate{}

	emitted := collect(g, "Colossus ", "is a ", "1970 film.")

	if emitted != "Colossus is a 1970 film." {
		t.Fatalf("emitted = %q, want full content", emitted)
	}
}

func TestToolCallStreamGate_FlushesTrailingLoneAngleBracket(t *testing.T) {
	g := &toolCallStreamGate{}

	// Content that ends mid-partial-marker must not be silently dropped.
	emitted := collect(g, "a < b is 1 <")

	if emitted != "a < b is 1 <" {
		t.Fatalf("emitted = %q, want unchanged", emitted)
	}
}
