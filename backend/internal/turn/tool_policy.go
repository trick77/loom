package turn

import (
	"regexp"
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
//     Prepare's drift detection).
//   - escalatedDocgen flags an explicit file-format request ("make a pdf",
//     "erstelle ein docx") so the file tools appear regardless of category.
//
// Both layers only ever add tools, so a mis-gate can at worst over-inject a few
// tokens, never strip a tool the turn needs.
type toolGate struct {
	category        string
	turnCategory    string
	escalatedDocgen bool
	// sandbox is whether run_python is offered this turn, decided once in
	// Prepare so the tool and its guidance never disagree when the health
	// probe flips mid-turn.
	sandbox bool
}

// newToolGate builds the gate from the sticky category, the fresh per-turn
// classification (may be empty when drift detection was skipped), and the current
// user message. Keyword matching is limited to explicit, language-spanning
// format nouns, as whole words (see docgenKeywords).
//
// The memory tools are not gated: tried with a keyword gate, a standing
// instruction phrased otherwise ("keine Emojis mehr", another language, a
// "yes" to the model's follow-up) lost its tool with no fallback (the
// background extraction takes facts only), and the memory blocks of the
// prompt name the tools on every turn.
func newToolGate(category, turnCategory, message string) toolGate {
	return toolGate{
		category:        category,
		turnCategory:    turnCategory,
		escalatedDocgen: docgenKeywords.MatchString(message),
	}
}

// widenAll is the safe fallback when there is no category signal at all — a
// legacy thread whose stored category is empty (migration 0013 default) and whose
// turn was not re-classified. Rather than silently gate every optional tool off
// (a regression versus the old always-inject behavior), offer everything.
func (g toolGate) widenAll() bool {
	return strings.TrimSpace(g.category) == "" && strings.TrimSpace(g.turnCategory) == ""
}

// docgenEnabled reports whether the file-generation tools (the ~9 KB schema
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
// of these, a per-turn drift re-classification would change nothing, so
// Prepare skips it.
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
// categories only get the file tools via an explicit format request. Coding,
// how-to and science/math are left out: long threads that rarely want a file
// (code is offered for download anyway), and each turn there carried the
// whole file-tool chunk.
var docgenCategories = map[classifier.Category]bool{
	classifier.WritingEditing:   true,
	classifier.Summarization:    true,
	classifier.CreativeWriting:  true,
	classifier.Planning:         true,
	classifier.Brainstorming:    true,
	classifier.AcademicResearch: true,
	classifier.Translation:      true,
}

// docgenKeywords escalate the file-generation tools on when the user explicitly
// names a downloadable format or artifact. These are explicit format nouns, not
// intent guesses, so they stay as a small lexical booster on top of the semantic
// category signal. Bilingual (English + Swiss German, no ß) so a German request
// ("erstelle eine Präsentation", "mach ein Word draus") gates the same as its
// English equivalent. Bare "file"/"datei" are excluded — too common in unrelated
// contexts — while the specific artifact/format words are unambiguous.
//
// Whole words, each with its own inflections, never substrings: a substring
// "word" escalated on "password", "document" on "documentation", "report" on
// "reporting", each pulling the whole file-tool chunk into the turn. German
// compounds count where the format word is a part (Exceldatei,
// Worddokument, Berichtsvorlage), so does a text format named with a file
// ("a yaml file", "als Markdown-Datei"); a bare format or "file" does not,
// both are everyday words in a coding thread.
var docgenKeywords = wholeWords(
	// formats / extensions (language-neutral); a compound starting with one
	`pdf\p{L}*`, `docx`, `xlsx`, `pptx`, `csv`, `excel\p{L}*`, `powerpoint\p{L}*`,
	// word processor / document; German compounds end in -dokument
	`documents?`, `\p{L}*dokumente?n?`, `word`,
	// spreadsheet
	`spreadsheets?`, `\p{L}*tabellen?`, `tabellen\p{L}+`,
	// presentation
	`presentations?`, `pr(?:ä|ae)sentation\p{L}*`, `slides`, `\p{L}*folien?`,
	// report; not "Berichterstattung" (news coverage)
	`reports?`, `\p{L}*bericht(?:e|en)?`, `berichts\p{L}+`,
	// export / download / save-as
	`export\p{L}*`, `download\p{L}*`, `herunterlad\p{L}*`, `save as`, `speichern? als`,
	`handouts?`,
	// a new file, named as one: "a yaml file", "a .md file", "in a file",
	// "Markdown-Datei", "in eine Datei". Only the indefinite article: "add
	// this to the file" is everyday coding talk.
	`\.?(?:md|markdown|json|ya?ml|txt|text|html|xml|log|config) files?`,
	`(?:in|into|to|as) (?:a|an) (?:\.?\p{L}+ )?files?`,
	`\p{L}+-?dateie?n?`, `(?:als|in eine?) datei`,
)

// wholeWords is a case-insensitive regexp matching any of alts as a whole
// word: no letter or digit right before or after.
func wholeWords(alts ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + strings.Join(alts, "|") + `)(?:$|[^\p{L}\p{N}])`)
}
