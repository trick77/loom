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
// parseInlineToolCalls extracts those blocks into structured ToolCalls and
// returns the content with the blocks removed. When no inline tool call is
// present it returns (nil, content) unchanged.
var (
	inlineToolCallBlock = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	inlineFunctionName  = regexp.MustCompile(`<function=([^>]+)>`)
	inlineParameter     = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)
)

func parseInlineToolCalls(content string) ([]ToolCall, string) {
	blocks := inlineToolCallBlock.FindAllStringSubmatchIndex(content, -1)
	if len(blocks) == 0 {
		return nil, content
	}

	var calls []ToolCall
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
			ID:   fmt.Sprintf("inline_call_%d", len(calls)+1),
			Type: "function",
			Function: ToolCallFunction{
				Name:      name,
				Arguments: inlineArguments(inner),
			},
		})
	}
	if len(calls) == 0 {
		return nil, content
	}

	cleaned := strings.TrimSpace(inlineToolCallBlock.ReplaceAllString(content, ""))
	return calls, cleaned
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
