package httpapi

import (
	"strings"

	"github.com/trick77/loom/internal/classifier"
)

// toolGate captures the per-turn signal that decides which optional tool groups
// are injected into the prompt. The base signal is the thread's classifier
// category (sticky: chosen on the first message, reused after). Two widen-only
// layers handle the cases where the sticky category is stale or missing:
//   - turnCategory is a fresh semantic classification of THIS turn's message,
//     used to re-enable coding-doc tools when a thread drifts into coding/how-to
//     (language-agnostic — the model reads the user's own words; see
//     message_stream_handler.go's drift detection).
//   - escalatedDocgen flags an explicit file-format request ("make a pdf",
//     "erstelle ein docx") so the file tools appear regardless of category.
//
// Both layers only ever add tools, so a mis-gate can at worst over-inject a few
// tokens, never strip a tool the turn needs.
type toolGate struct {
	category        string
	turnCategory    string
	escalatedDocgen bool
}

// newToolGate builds the gate from the sticky category, the fresh per-turn
// classification (may be empty when drift detection was skipped), and the current
// user message. Keyword matching is substring-based on the lowercased message so
// it tolerates inflection and is limited to explicit, language-spanning format
// nouns (see docgenKeywords).
func newToolGate(category, turnCategory, message string) toolGate {
	return toolGate{
		category:        category,
		turnCategory:    turnCategory,
		escalatedDocgen: containsAny(strings.ToLower(message), docgenKeywords),
	}
}

// widenAll is the safe fallback when there is no category signal at all — a
// legacy thread whose stored category is empty (migration 0013 default) and whose
// turn was not re-classified. Rather than silently gate every optional tool off
// (a regression versus the old always-inject behavior), offer everything.
func (g toolGate) widenAll() bool {
	return strings.TrimSpace(g.category) == "" && strings.TrimSpace(g.turnCategory) == ""
}

// docgenEnabled reports whether the file-generation tools (the ~7 KB schema
// chunk) should be injected: no category signal at all, an explicit file request
// in the message, or a document-plausible sticky/turn category.
func (g toolGate) docgenEnabled() bool {
	if g.widenAll() || g.escalatedDocgen {
		return true
	}
	return docgenCategories[classifier.Normalize(g.category)] ||
		docgenCategories[classifier.Normalize(g.turnCategory)]
}

// activeCategories is the set of classifier categories in force for MCP gating:
// the sticky category plus the fresh per-turn category. A server tagged e.g.
// "coding" (context7) is thus injected both for coding-classified threads and for
// any turn a fresh classification judged coding/how-to.
func (g toolGate) activeCategories() map[string]bool {
	active := map[string]bool{}
	for _, c := range []string{g.category, g.turnCategory} {
		if c = strings.TrimSpace(c); c != "" {
			active[c] = true
		}
	}
	return active
}

// codingDocCategories are the categories that already expose the coding-doc MCP
// servers (context7 is tagged coding + how_to). When the sticky category is one
// of these, a per-turn drift re-classification would change nothing, so the
// handler skips it.
var codingDocCategories = map[classifier.Category]bool{
	classifier.Coding: true,
	classifier.HowTo:  true,
}

// categoryGrantsCodingDocs reports whether the sticky category already activates
// the coding-doc servers, letting the caller skip drift re-classification.
func categoryGrantsCodingDocs(category string) bool {
	if strings.TrimSpace(category) == "" {
		return false
	}
	return codingDocCategories[classifier.Normalize(category)]
}

// docgenCategories are the classifier categories for which a user asking to
// produce a downloadable document/spreadsheet/presentation is plausible. Other
// categories only get the file tools via an explicit format request. The
// FileToolGuardrail already stops the model from creating files unprompted, so
// this list can stay generous without spurious artifacts.
var docgenCategories = map[classifier.Category]bool{
	classifier.WritingEditing:   true,
	classifier.Summarization:    true,
	classifier.CreativeWriting:  true,
	classifier.Planning:         true,
	classifier.Brainstorming:    true,
	classifier.AcademicResearch: true,
	classifier.HowTo:            true,
	classifier.Coding:           true,
	classifier.ScienceMath:      true,
	classifier.Translation:      true,
}

// docgenKeywords escalate the file-generation tools on when the user explicitly
// names a downloadable format or artifact. These are explicit format nouns, not
// intent guesses, so they stay as a small lexical booster on top of the semantic
// category signal. Bilingual (English + Swiss German, no ß) so a German request
// ("erstelle eine Präsentation", "mach ein Word draus") gates the same as its
// English equivalent. Bare "file"/"datei" are excluded — too common in unrelated
// contexts — while the specific artifact/format words are unambiguous.
var docgenKeywords = []string{
	// formats / extensions (language-neutral)
	"pdf", "docx", "xlsx", "pptx", "csv",
	// word processor / document
	"document", "dokument", "word",
	// spreadsheet
	"spreadsheet", "tabelle", "tabellenkalkulation", "excel",
	// presentation
	"presentation", "präsentation", "praesentation", "powerpoint", "slides", "folien",
	// report
	"report", "bericht",
	// export / download / save-as
	"export", "exportier", "download", "herunterlad", "save as", "speichern als", "speicher als",
	"handout",
}

// containsAny reports whether lower (already lowercased) contains any needle.
func containsAny(lower string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
