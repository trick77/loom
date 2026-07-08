package httpapi

import (
	"strings"

	"github.com/trick77/loom/internal/llm"
)

// finalSynthesisNotesBudgetTokens caps the research notes folded into the forced
// final-answer turn. Sized so the synthesis call stays within the context the model
// handles reliably (the tool rounds themselves ran ~20k-token prompts) and well
// under MiMo's window; when the gathered notes exceed it the oldest are dropped.
const finalSynthesisNotesBudgetTokens = 24000

// buildFinalSynthesisHistory rebuilds the forced final-answer turn as a clean,
// tool-free turn over the research notes already gathered. After a tool-saturated
// research turn MiMo will not commit to prose when simply told to — it reflexively
// emits another (unrunnable) tool call, which is stripped to empty and dead-ends the
// turn. Every clean, tool-free single-purpose call in the codebase (thread titles,
// classification, project descriptions/memory) reliably gets prose out of the same
// model, and they all share one shape: a fresh [system, user-with-material] history.
// This mirrors that shape for the final answer.
//
// It keeps the original system prompt + prior conversation + the user's question
// (history[:baseLen]), drops every assistant(tool_calls)+tool(result) pair — the
// pattern that triggers the tool-call reflex — and folds the gathered tool outputs
// (already [n]-annotated web notes) into the final user message. Returns ok=false
// when there are no notes, so the caller keeps the previous full-history behaviour.
func buildFinalSynthesisHistory(history []llm.Message, baseLen int, extraDirective string) ([]llm.Message, bool) {
	if baseLen <= 0 || baseLen > len(history) {
		return nil, false
	}
	notes := collectToolNotes(history[baseLen:], finalSynthesisNotesBudgetTokens*bytesPerToken)
	if notes == "" {
		return nil, false
	}
	base := history[:baseLen]
	last := base[len(base)-1]

	instruction := "Using the research notes above, write your complete final answer now, in the user's language. Do not call any tools."
	if strings.TrimSpace(extraDirective) != "" {
		instruction += " " + strings.TrimSpace(extraDirective)
	}
	notesBlock := "\n\n---\nResearch notes gathered to answer this question:\n\n" + notes + "\n---\n\n" + instruction

	synth := make([]llm.Message, 0, baseLen+1)
	// Fold the notes into the final user message so the turn is a single user turn
	// (system + prior + one augmented user), exactly like the utility calls. An image
	// question carries ContentParts instead of text, so append notes as a separate
	// user turn rather than clobbering the parts.
	if last.Role == "user" && len(last.ContentParts) == 0 {
		synth = append(synth, base[:len(base)-1]...)
		synth = append(synth, llm.Message{Role: "user", Content: last.Content + notesBlock})
	} else {
		synth = append(synth, base...)
		synth = append(synth, llm.Message{Role: "user", Content: notesBlock})
	}
	return synth, true
}

// collectToolNotes concatenates the Content of every role:"tool" message in order,
// capped to maxBytes. When over budget it keeps the tail (the most recent research),
// so the freshest sources survive rather than being truncated away.
func collectToolNotes(rounds []llm.Message, maxBytes int) string {
	var parts []string
	for _, m := range rounds {
		if m.Role == "tool" && strings.TrimSpace(m.Content) != "" {
			parts = append(parts, strings.TrimSpace(m.Content))
		}
	}
	joined := strings.Join(parts, "\n\n")
	if maxBytes > 0 && len(joined) > maxBytes {
		// Keep the tail, but the byte cut can land mid-rune (these notes are full of
		// umlauts/ß); ToValidUTF8 drops the leading partial rune so the folded text is
		// always valid UTF-8.
		joined = strings.ToValidUTF8(joined[len(joined)-maxBytes:], "")
	}
	return joined
}
