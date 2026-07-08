package httpapi

import (
	"encoding/json"
	"strings"

	"github.com/trick77/loom/internal/llm"
)

// finalAnswerToolName is the single tool offered on the forced final-answer turn.
// After a research turn exhausts the round budget, MiMo will not commit to prose
// when simply told to: it compulsively emits another, unrunnable tool call that the
// inline parser strips to empty (the failure the forced-final path exists to
// defeat). Handing a tool-eager model a tool whose whole job is to carry the answer
// gives it a channel it will actually use — the inline-tool-call parser already
// recovers the call, and adoptFinalAnswer lifts the answer back out as the turn's
// prose content.
const finalAnswerToolName = "final_answer"

// finalAnswerTool is the schema advertised on the forced final-answer turn.
func finalAnswerTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: finalAnswerToolName,
			Description: "Deliver your complete, final answer to the user. Call this exactly once, " +
				"passing the full answer as the text argument. This is the only way to respond on " +
				"this turn — do not call any other tool.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type": "string",
						"description": "The complete final answer to show the user, in plain prose or " +
							"markdown, written in the user's language.",
					},
				},
				"required": []any{"text"},
			},
		},
	}
}

// adoptFinalAnswer folds a final_answer tool call into the turn's prose content so
// the answer renders as a normal text block rather than a tool invocation. A direct
// prose answer (non-empty Content that is not raw tool-call markup) always wins;
// otherwise the first final_answer call whose text argument is non-empty becomes the
// content and that one call is dropped from ToolCalls so it leaves no orphan
// tool-call block in the trace. Any other (non-final, unrunnable) tool calls are left
// in place, and an empty result is returned unchanged — the caller's empty-content
// check then drives the retry and fallback exactly as before.
func adoptFinalAnswer(result llm.StreamResult) llm.StreamResult {
	if text := strings.TrimSpace(result.Content); text != "" && !inlineToolMarkup(text) {
		return result
	}
	for i, call := range result.ToolCalls {
		if call.Function.Name != finalAnswerToolName {
			continue
		}
		text := parseFinalAnswerText(call.Function.Arguments)
		if text == "" {
			continue
		}
		result.Content = text
		result.ToolCalls = append(append([]llm.ToolCall(nil), result.ToolCalls[:i]...), result.ToolCalls[i+1:]...)
		return result
	}
	// No usable final_answer, and any content is raw tool-call markup (e.g. a call
	// truncated at the token cap, which the client normally scrubs to empty). Never
	// surface raw XML as the answer: blank it so the caller's empty-content check
	// drives the retry/fallback instead.
	if inlineToolMarkup(result.Content) {
		result.Content = ""
	}
	return result
}

// inlineToolMarkup reports whether s still carries raw inline tool-call markup — the
// <tool_call>/<tool_invocation> forms the llm client normally scrubs from content
// (see cutAtFirstInlineMarker). This is a defensive guard so a truncated or
// unscrubbed call can never leak raw XML into the answer bubble.
func inlineToolMarkup(s string) bool {
	return strings.Contains(s, "<tool_call>") || strings.Contains(s, "<tool_invocation")
}

// parseFinalAnswerText extracts the text argument from a final_answer call's JSON
// arguments. A truncated or malformed payload (e.g. a finish_reason=length cut-off)
// unmarshals to empty, which the caller treats as "no usable answer" and retries.
func parseFinalAnswerText(arguments string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Text)
}
