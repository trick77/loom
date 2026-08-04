package httpapi

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// The fields of a web citation a public share may carry. Everything else the stored
// citation holds — documentId, score, full — is either meaningless to a reader or
// tells them about the owner's retrieval setup, so it is dropped. favicon is dropped
// too: the viewer resolves icons from the url through its own proxy.
//
// Listed as a whitelist rather than a blocklist so a new field added to the stored
// citation cannot start leaking by default.
var shareCitationFields = []string{"url", "title", "filename", "snippet", "index"}

// projectCitationsForShare keeps the web citations of a message — those pointing at a
// public http(s) page — projects each to the fields above, and reports which [n]
// markers survived.
//
// Anything else is a RAG document: the owner's private upload. It is dropped whole,
// and its marker is then stripped from the prose by stripDroppedMarkers, because a
// marker with no source behind it renders as literal "[4]" text in the answer — a
// dead bracket the reader cannot act on.
func projectCitationsForShare(raw json.RawMessage) (json.RawMessage, map[int]bool) {
	kept := map[int]bool{}
	if isEmptyJSON(raw) {
		return nil, kept
	}
	var citations []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &citations); err != nil {
		// A malformed citations blob is not worth failing a share over: drop them all
		// and let the markers be stripped, which is the safe direction.
		return nil, kept
	}

	projected := make([]map[string]json.RawMessage, 0, len(citations))
	for _, citation := range citations {
		if !isPublicWebURL(citation["url"]) {
			continue
		}
		out := map[string]json.RawMessage{}
		for _, field := range shareCitationFields {
			if value, ok := citation[field]; ok {
				out[field] = value
			}
		}
		projected = append(projected, out)
		if index, ok := citationIndex(citation); ok {
			kept[index] = true
		}
	}
	if len(projected) == 0 {
		return nil, kept
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return nil, map[int]bool{}
	}
	return encoded, kept
}

// isPublicWebURL reports whether a citation's url is a page a logged-out reader could
// fetch themselves — the whole justification for letting it out of the share.
//
// The scheme check is the gate, not a formality. "a citation with a url is a web
// source" is the UI's rule for *display*, and it is too loose for a security boundary:
// a document citation can carry an internal locator (doc://…, file://…) that is still
// the name of something private. Only http and https qualify.
func isPublicWebURL(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func citationIndex(obj map[string]json.RawMessage) (int, bool) {
	raw, ok := obj["index"]
	if !ok {
		return 0, false
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// stripDroppedMarkers removes the "[n]" markers whose source did not survive the
// projection, along with the space in front of them, so a shared answer never shows a
// bracket pointing at nothing.
//
// Only markers the message actually cited are touched: `cited` is the full set of
// indices the stored citations carried, and anything outside it is left alone. That
// keeps "[0]" and other bracketed text — a footnote the model wrote by hand, a bare
// array subscript in prose — exactly as written.
//
// Code is skipped for the same reason the UI's numbering pass skips it: "arr[1]"
// inside a fence is an index expression, and rewriting it would corrupt code that a
// reader is meant to copy.
func stripDroppedMarkers(content string, cited, kept map[int]bool) string {
	if content == "" || len(cited) == 0 {
		return content
	}
	drop := func(index int) bool { return cited[index] && !kept[index] }
	if !anyDropped(cited, kept) {
		return content
	}

	var out strings.Builder
	out.Grow(len(content))
	for _, segment := range splitCodeSegments(content) {
		if segment.code {
			out.WriteString(segment.text)
			continue
		}
		out.WriteString(stripMarkersInProse(segment.text, drop))
	}
	return out.String()
}

func anyDropped(cited, kept map[int]bool) bool {
	for index := range cited {
		if !kept[index] {
			return true
		}
	}
	return false
}

// stripMarkersInProse rewrites one non-code stretch, dropping each "[n]" the caller
// rejects together with the blanks directly in front of it.
func stripMarkersInProse(text string, drop func(int) bool) string {
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] != '[' {
			out = append(out, text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], ']')
		digits := ""
		if end > 0 {
			digits = text[i+1 : i+end]
		}
		index, err := strconv.Atoi(digits)
		if end < 0 || digits == "" || err != nil || !drop(index) {
			out = append(out, text[i])
			i++
			continue
		}
		// Take the blanks in front with it: the marker hugs the word it backed, and
		// leaving the space would open a hole mid-sentence.
		//
		// Unless another marker follows immediately. In "agree [1][2]" the blank
		// belongs to the run, not to the marker being dropped, and eating it would
		// weld the survivor onto the word: "agree[2]".
		if i+end+1 >= len(text) || text[i+end+1] != '[' {
			for len(out) > 0 && (out[len(out)-1] == ' ' || out[len(out)-1] == '\t') {
				out = out[:len(out)-1]
			}
		}
		i += end + 1
	}
	return string(out)
}

type codeSegment struct {
	text string
	code bool
}

// splitCodeSegments cuts text into alternating prose and code stretches. It handles
// what the UI's stripCode handles: ``` and ~~~ fences (closed or still unterminated),
// indented blocks, and inline backtick spans of any run length.
func splitCodeSegments(content string) []codeSegment {
	var segments []codeSegment
	var prose strings.Builder
	flushProse := func() {
		if prose.Len() > 0 {
			segments = append(segments, codeSegment{text: prose.String()})
			prose.Reset()
		}
	}

	lines := strings.SplitAfter(content, "\n")
	fence := ""
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case fence != "":
			segments = append(segments, codeSegment{text: line, code: true})
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
			flushProse()
			fence = trimmed[:3]
			segments = append(segments, codeSegment{text: line, code: true})
		case strings.HasPrefix(line, "    "), strings.HasPrefix(line, "\t"):
			flushProse()
			segments = append(segments, codeSegment{text: line, code: true})
		default:
			for _, part := range splitInlineCode(line) {
				if part.code {
					flushProse()
					segments = append(segments, part)
					continue
				}
				prose.WriteString(part.text)
			}
		}
	}
	flushProse()
	return segments
}

// splitInlineCode separates `code` spans from the prose around them within one line.
// A span closes on a run of backticks of the same length; an unclosed run runs to the
// end of the line, matching how markdown renders a partial span mid-stream.
func splitInlineCode(line string) []codeSegment {
	var segments []codeSegment
	for i := 0; i < len(line); {
		if line[i] != '`' {
			start := i
			for i < len(line) && line[i] != '`' {
				i++
			}
			segments = append(segments, codeSegment{text: line[start:i]})
			continue
		}
		start := i
		for i < len(line) && line[i] == '`' {
			i++
		}
		run := line[start:i]
		closing := strings.Index(line[i:], run)
		if closing < 0 {
			segments = append(segments, codeSegment{text: line[start:], code: true})
			break
		}
		i += closing + len(run)
		segments = append(segments, codeSegment{text: line[start:i], code: true})
	}
	return segments
}

// citedIndices collects every [n] the stored citations carried, web and document
// alike — the set stripDroppedMarkers is allowed to touch.
func citedIndices(raw json.RawMessage) map[int]bool {
	indices := map[int]bool{}
	if isEmptyJSON(raw) {
		return indices
	}
	var citations []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &citations); err != nil {
		return indices
	}
	for _, citation := range citations {
		if index, ok := citationIndex(citation); ok {
			indices[index] = true
		}
	}
	return indices
}
