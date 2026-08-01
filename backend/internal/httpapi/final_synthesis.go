package httpapi

import (
	"fmt"
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
func buildFinalSynthesisHistory(history []llm.Message, baseLen int, extraDirective string, sources []webSource) ([]llm.Message, bool) {
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
	// Restate the citation rule here. It is also in the system prompt, but that sits
	// far up the history while the notes block is this turn's immediate context —
	// and this turn produces most answers that follow a research round. Worded to
	// match loomSystemPrompt and kept language-neutral (the answer may be DE/FR/IT).
	if len(sources) > 0 {
		instruction += " The research notes are labeled with bracketed numbers like [1] or [2]." +
			" Whenever a sentence in your answer draws on one of these web sources, append its marker" +
			" at the end of that sentence — [1], or several like [1][3]. Use only numbers listed above;" +
			" never invent a citation number." +
			// Left to itself the model closes with its own "Sources:" list of bare URLs.
			// The interface already renders every cited source as a numbered, linked list
			// under the answer, so that block is pure duplication — and a wall of raw
			// URLs at that.
			" Do not end your answer with a list of sources, references or URLs:" +
			" the interface already shows the full source list to the user."
	}
	if strings.TrimSpace(extraDirective) != "" {
		instruction += " " + strings.TrimSpace(extraDirective)
	}
	// The index goes ahead of the notes so the marker -> source mapping survives even
	// when the notes themselves were truncated to fit the budget.
	notesBlock := "\n\n---\n" + webSourceIndexBlock(sources) +
		"Research notes gathered to answer this question:\n\n" + notes + "\n---\n\n" + instruction

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

// truncationMarker terminates a note that was cut to fit its share of the budget,
// so the model can tell "this source said nothing more" from "this source was cut".
const truncationMarker = "\n…[note truncated]"

// collectToolNotes concatenates the Content of every role:"tool" message in order,
// capped to roughly maxBytes.
//
// Over budget, each note is trimmed to its own fair share rather than the whole
// join being tail-cut. A global tail cut deleted the earliest notes outright —
// including their "Web source [n]:" headers — so the model was asked to cite
// [1]..[n] while it could only see the last few, which is a direct cause of
// answers that cite nothing at all. Fair-share keeps every source represented.
//
// Each note keeps its *head*, because that is where the "Web source [n]: url"
// header sits; losing it strands the citation marker.
func collectToolNotes(rounds []llm.Message, maxBytes int) string {
	var parts []string
	for _, m := range rounds {
		if m.Role == "tool" && strings.TrimSpace(m.Content) != "" {
			parts = append(parts, strings.TrimSpace(m.Content))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if maxBytes > 0 && len(strings.Join(parts, "\n\n")) > maxBytes {
		parts = shareBudget(parts, maxBytes)
	}
	return strings.Join(parts, "\n\n")
}

// shareBudget trims parts so their total lands near maxBytes: every part may keep
// at least an equal share, and parts smaller than their share donate the remainder
// to the oversized ones (split evenly among them).
func shareBudget(parts []string, maxBytes int) []string {
	share := maxBytes / len(parts)
	surplus := 0
	var over []int
	for i, p := range parts {
		if len(p) <= share {
			surplus += share - len(p)
			continue
		}
		over = append(over, i)
	}
	if len(over) == 0 {
		return parts
	}
	bonus := surplus / len(over)
	out := append([]string(nil), parts...)
	for _, i := range over {
		out[i] = truncateHead(out[i], share+bonus)
	}
	return out
}

// truncateHead keeps the first keep bytes of s, counting the appended marker
// against that allowance so the result never exceeds keep. The cut can land
// mid-rune (these notes are full of umlauts/ß), so ToValidUTF8 drops the trailing
// partial rune.
func truncateHead(s string, keep int) string {
	if keep <= 0 || len(s) <= keep {
		return s
	}
	body := keep - len(truncationMarker)
	if body <= 0 {
		return strings.ToValidUTF8(s[:keep], "")
	}
	return strings.ToValidUTF8(s[:body], "") + truncationMarker
}

// webSourceIndexBlock renders the complete list of gathered sources as a compact
// "[n] Label — url" index. It is prepended to the folded research notes so the
// marker -> source mapping is present even when the notes were truncated: without
// it, a cut note takes its "Web source [n]:" header with it and the model can no
// longer tell what [n] refers to. Roughly 15 tokens per source.
func webSourceIndexBlock(sources []webSource) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Web sources gathered (cite with [n]):\n")
	for _, s := range sources {
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = strings.TrimSpace(s.Title)
		}
		fmt.Fprintf(&b, "[%d] %s — %s\n", s.Index, label, strings.TrimSpace(s.URL))
	}
	b.WriteString("\n")
	return b.String()
}
