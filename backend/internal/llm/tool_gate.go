package llm

import "strings"

const (
	inlineToolCallMarker   = "<tool_call>"
	inlineInvocationMarker = "<tool_invocation"
)

// inlineToolCallMarkers are the opening markers of every inline tool-call syntax
// MiMo is known to emit instead of native tool_calls: the documented
// <tool_call>…</tool_call> form and the <tool_invocation name=… arguments={…} />
// variant it sometimes invents. The gate suppresses streamed content from the
// earliest of these so no raw markup of either form reaches the client.
var inlineToolCallMarkers = []string{inlineToolCallMarker, inlineInvocationMarker}

// containsInlineToolMarker reports whether s contains any inline tool-call marker.
func containsInlineToolMarker(s string) bool {
	for _, m := range inlineToolCallMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// cutAtFirstInlineMarker returns s truncated at the earliest inline tool-call marker,
// mirroring what the stream gate withholds live. It is the persistence-side safety net:
// when a tool-call block fails to parse (truncated/malformed) the parser leaves the raw
// markup in place, and this keeps that markup out of stored content so a reloaded thread
// never shows it. A no-op when no marker is present (the common, fully-parsed case).
func cutAtFirstInlineMarker(s string) string {
	cut := -1
	for _, m := range inlineToolCallMarkers {
		if idx := strings.Index(s, m); idx >= 0 && (cut < 0 || idx < cut) {
			cut = idx
		}
	}
	if cut < 0 {
		return s
	}
	return strings.TrimSpace(s[:cut])
}

// toolCallStreamGate buffers streamed content for models that emit tool calls as
// inline XML (e.g. MiMo), so the raw markup is never streamed to the client.
// Normal content still streams token-by-token; only a (potential) tool call is
// held back. Once a full marker is seen, the remainder is suppressed, since MiMo
// emits nothing but tool calls after the first one.
type toolCallStreamGate struct {
	buffer     string
	suppressed bool
}

// push returns the portion of delta that is safe to stream now.
func (g *toolCallStreamGate) push(delta string) string {
	if g.suppressed {
		return ""
	}
	g.buffer += delta
	// Suppress from the earliest complete marker of any known inline syntax.
	earliest := -1
	for _, m := range inlineToolCallMarkers {
		if idx := strings.Index(g.buffer, m); idx >= 0 && (earliest < 0 || idx < earliest) {
			earliest = idx
		}
	}
	if earliest >= 0 {
		out := g.buffer[:earliest]
		g.buffer = ""
		g.suppressed = true
		return out
	}
	// Hold back a trailing suffix that could be the start of any marker.
	hold := 0
	for _, m := range inlineToolCallMarkers {
		if h := partialMarkerSuffixLen(g.buffer, m); h > hold {
			hold = h
		}
	}
	out := g.buffer[:len(g.buffer)-hold]
	g.buffer = g.buffer[len(g.buffer)-hold:]
	return out
}

// flush returns any remaining buffered content, called once the stream ends.
// A held suffix that never grew into a marker is real content and must surface.
func (g *toolCallStreamGate) flush() string {
	if g.suppressed {
		return ""
	}
	out := g.buffer
	g.buffer = ""
	return out
}

// partialMarkerSuffixLen returns the length of the longest suffix of s that is a
// prefix of marker (e.g. "abc<too" with marker "<tool_call>" returns 4).
func partialMarkerSuffixLen(s, marker string) int {
	max := len(marker) - 1
	if len(s) < max {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}
