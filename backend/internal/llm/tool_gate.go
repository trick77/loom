package llm

import "strings"

const inlineToolCallMarker = "<tool_call>"

// toolCallStreamGate buffers streamed content for models that emit tool calls as
// inline XML (e.g. MiMo), so the raw <tool_call> markup is never streamed to the
// client. Normal content still streams token-by-token; only a (potential) tool
// call is held back. Once a full marker is seen, the remainder is suppressed,
// since MiMo emits nothing but tool calls after the first one.
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
	if idx := strings.Index(g.buffer, inlineToolCallMarker); idx >= 0 {
		out := g.buffer[:idx]
		g.buffer = ""
		g.suppressed = true
		return out
	}
	// Hold back a trailing suffix that could be the start of the marker.
	hold := partialMarkerSuffixLen(g.buffer, inlineToolCallMarker)
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
