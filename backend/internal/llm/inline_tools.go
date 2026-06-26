package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Some models (notably MiMo) emit tool calls as inline XML inside the assistant
// content stream instead of populating the OpenAI-native tool_calls field, e.g.:
//
//	<tool_call>
//	<function=tavily__tavily_search>
//	<parameter=query>colossus forbin project</parameter>
//	</function>
//	</tool_call>
//
// MiMo also sometimes invents a second, undocumented syntax that no prompt teaches:
//
//	<tool_invocation name="generate_image" arguments={"prompt": "…", "height": 1024} />
//
// parseInlineToolCalls extracts blocks of either syntax into structured ToolCalls and
// returns the content with the blocks removed. When no inline tool call is present it
// returns (nil, content) unchanged.
var (
	inlineToolCallBlock = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	inlineFunctionName  = regexp.MustCompile(`<function=([^>]+)>`)
	inlineParameter     = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)

	// For the <tool_invocation …/> variant. The arguments value is raw JSON containing
	// nested braces and quotes, so it is located by balanced-brace scanning (see
	// scanJSONObject) rather than a regex; only the name attribute is matched here. The
	// \b anchors the match to the `name` attribute so a name-suffixed attribute ahead of
	// it (display_name="x" …) can't be mistaken for the tool name.
	inlineInvocationName = regexp.MustCompile(`\bname\s*=\s*"([^"]*)"`)
	inlineInvocationArgs = regexp.MustCompile(`arguments\s*=\s*`)
)

// inlineToolCallID is the synthetic id assigned to the index-th (0-based) inline
// tool call. Streaming surfaces the first call's name early under this same id (see
// firstInlineToolName), so the full call parsed at end-of-stream updates that entry
// instead of creating a duplicate.
func inlineToolCallID(index int) string {
	return fmt.Sprintf("inline_call_%d", index+1)
}

// firstInlineToolName extracts the function name of the first inline tool call from
// a (possibly still-streaming) content buffer, as soon as the <function=NAME> tag
// has fully arrived. MiMo emits the name right after the <tool_call> marker but only
// flushes the large argument tens of seconds later, so this lets the client show
// which tool is running during that silent gap. Returns "" until the name is parseable.
func firstInlineToolName(content string) string {
	if match := inlineFunctionName.FindStringSubmatch(content); match != nil {
		return strings.TrimSpace(match[1])
	}
	// The <tool_invocation …/> variant carries the name as an attribute; surface it the
	// same way once the name="…" attribute has fully arrived.
	if idx := strings.Index(content, inlineInvocationMarker); idx >= 0 {
		if match := inlineInvocationName.FindStringSubmatch(content[idx:]); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func parseInlineToolCalls(content string) ([]ToolCall, string) {
	var calls []ToolCall
	cleaned := content

	// Syntax 1: <tool_call><function=NAME><parameter=k>v</parameter>…</tool_call>.
	if blocks := inlineToolCallBlock.FindAllStringSubmatchIndex(content, -1); len(blocks) > 0 {
		for _, block := range blocks {
			inner := content[block[2]:block[3]]
			nameMatch := inlineFunctionName.FindStringSubmatch(inner)
			if nameMatch == nil {
				continue
			}
			name := strings.TrimSpace(nameMatch[1])
			if name == "" {
				continue
			}
			calls = append(calls, ToolCall{
				Type: "function",
				Function: ToolCallFunction{
					Name:      name,
					Arguments: inlineArguments(inner),
				},
			})
		}
		if len(calls) > 0 {
			cleaned = inlineToolCallBlock.ReplaceAllString(cleaned, "")
		}
	}

	// Syntax 2: <tool_invocation name="NAME" arguments={…json…} />.
	if invCalls, invCleaned := parseInvocationTags(cleaned); len(invCalls) > 0 {
		calls = append(calls, invCalls...)
		cleaned = invCleaned
	}

	if len(calls) == 0 {
		return nil, content
	}
	// Assign ids by overall order so the first call keeps the id streaming surfaced
	// early (inlineToolCallID(0)) regardless of which syntax produced it.
	for i := range calls {
		calls[i].ID = inlineToolCallID(i)
	}
	return calls, strings.TrimSpace(cleaned)
}

// parseInvocationTags extracts every <tool_invocation name="…" arguments={…} /> tag,
// returning the parsed calls and the content with those tags removed. Tags that don't
// parse cleanly (no name, truncated, unbalanced JSON) are left in place so the
// end-of-stream "unparsed markup" warning can surface them.
func parseInvocationTags(content string) ([]ToolCall, string) {
	// Fast path: the vast majority of completions carry no variant marker, so skip
	// allocating/copying the whole body into a builder when there's nothing to find.
	if !strings.Contains(content, inlineInvocationMarker) {
		return nil, content
	}
	var calls []ToolCall
	var b strings.Builder
	i := 0
	for {
		rel := strings.Index(content[i:], inlineInvocationMarker)
		if rel < 0 {
			b.WriteString(content[i:])
			break
		}
		start := i + rel
		call, end, ok := parseInvocationAt(content, start)
		if !ok {
			// Keep the marker text and advance past it so the scan can't loop forever.
			b.WriteString(content[i : start+len(inlineInvocationMarker)])
			i = start + len(inlineInvocationMarker)
			continue
		}
		b.WriteString(content[i:start])
		calls = append(calls, call)
		i = end
	}
	return calls, b.String()
}

// parseInvocationAt parses a single <tool_invocation …/> tag beginning at start.
// It returns the call and the index just past the tag's closing '>'. ok is false when
// the tag has no usable name, never closes, or carries a present-but-malformed
// arguments value (so the caller leaves the raw markup for the unparsed-markup warning
// rather than dispatching a degenerate call).
func parseInvocationAt(content string, start int) (ToolCall, int, bool) {
	seg := content[start:]

	nameMatch := inlineInvocationName.FindStringSubmatchIndex(seg)
	if nameMatch == nil {
		return ToolCall{}, 0, false
	}
	name := strings.TrimSpace(seg[nameMatch[2]:nameMatch[3]])
	if name == "" {
		return ToolCall{}, 0, false
	}

	// The first '>' after the name attribute closes a no-arguments tag, and for a tag
	// with arguments it serves as a sentinel: this tag's own arguments= sits before it
	// (the '>' is then inside the JSON), whereas a *later* tag's arguments= sits after
	// it. That distinction replaces an earlier marker-literal bound that wrongly
	// truncated tags whose JSON contained the literal "<tool_invocation".
	gt := strings.IndexByte(seg[nameMatch[1]:], '>')
	firstGt := -1
	if gt >= 0 {
		firstGt = nameMatch[1] + gt
	}

	// arguments={…}: the value is raw JSON, located by brace scan. A present-but-broken
	// value (truncated, single-quoted, trailing junk) must NOT silently degrade to an
	// empty object and dispatch a prompt-less image — fail the tag instead. A genuinely
	// absent arguments attribute faithfully degrades to {}.
	args := "{}"
	afterAttrs := nameMatch[1]
	if loc := inlineInvocationArgs.FindStringIndex(seg); loc != nil && (firstGt < 0 || loc[0] < firstGt) {
		valStart := loc[1]
		if valStart >= len(seg) || seg[valStart] != '{' {
			return ToolCall{}, 0, false
		}
		jsonEnd, ok := scanJSONObject(seg, valStart)
		if !ok {
			return ToolCall{}, 0, false
		}
		raw := seg[valStart:jsonEnd]
		if !json.Valid([]byte(raw)) {
			return ToolCall{}, 0, false
		}
		args = raw
		afterAttrs = jsonEnd
	}

	// The tag closes at the first '>' after the attributes (covers both "/>" and ">").
	closeRel := strings.IndexByte(seg[afterAttrs:], '>')
	if closeRel < 0 {
		return ToolCall{}, 0, false
	}
	end := start + afterAttrs + closeRel + 1
	return ToolCall{
		Type:     "function",
		Function: ToolCallFunction{Name: name, Arguments: args},
	}, end, true
}

// scanJSONObject returns the index just past the '}' that closes the object opening at
// s[open] (which must be '{'), respecting quoted strings and escapes so braces inside
// string values don't end the scan early. ok is false if the object never closes.
func scanJSONObject(s string, open int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// inlineArguments renders the <parameter=key>value</parameter> pairs of a single
// tool-call block as a JSON object, preserving the order in which they appear.
// Values stay strings; downstream tool argument coercion handles typing.
func inlineArguments(inner string) string {
	params := inlineParameter.FindAllStringSubmatch(inner, -1)
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range params {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONString(&b, strings.TrimSpace(p[1]))
		b.WriteByte(':')
		writeJSONString(&b, strings.TrimSpace(p[2]))
	}
	b.WriteByte('}')
	return b.String()
}

// writeJSONString encodes s as a JSON string without HTML escaping, so query
// values keep readable &, <, > characters instead of \u00xx sequences.
func writeJSONString(b *strings.Builder, s string) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	// Encode appends a trailing newline; trim it back off.
	b.WriteString(strings.TrimRight(buf.String(), "\n"))
}
