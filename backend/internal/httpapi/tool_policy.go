package httpapi

import (
	"strings"

	"github.com/trick77/loom/internal/classifier"
)

// toolGate captures the per-turn signal that decides which optional tool groups
// are injected into the prompt. The base signal is the thread's classifier
// category (sticky: chosen on the first message, reused after). Keyword
// escalation over the current user message can only *widen* the set — it flips
// groups on, never off — so a mis-gate at worst over-injects a few tokens and
// never strips a tool the turn needs. This mirrors image_heuristics.go's
// per-message heuristic layer and is the answer to mid-thread topic drift (a
// thread that opened as chit-chat but turns to coding still gets coding tools).
type toolGate struct {
	category        string
	escalatedDocgen bool
	escalatedCoding bool
}

// newToolGate builds the gate from the sticky category and the current user
// message. message is lowercased once; keyword matching is substring-based so it
// tolerates inflection (German "exportieren"/"exportiere" both match "exportier").
func newToolGate(category, message string) toolGate {
	lower := strings.ToLower(message)
	return toolGate{
		category:        category,
		escalatedDocgen: containsAny(lower, docgenKeywords),
		escalatedCoding: containsAny(lower, codingKeywords),
	}
}

// docgenEnabled reports whether the file-generation tools (the ~7 KB schema
// chunk) should be injected: either the sticky category plausibly produces a
// downloadable file, or the current message explicitly asks for one.
func (g toolGate) docgenEnabled() bool {
	return g.escalatedDocgen || docgenCategories[classifier.Normalize(g.category)]
}

// activeCategories is the set of classifier categories in force for MCP gating:
// the sticky category plus any escalated categories. context7 (tagged "coding")
// is thus injected both for threads classified as coding and for any turn whose
// wording shows coding intent.
func (g toolGate) activeCategories() map[string]bool {
	active := map[string]bool{}
	if c := strings.TrimSpace(g.category); c != "" {
		active[c] = true
	}
	if g.escalatedCoding {
		active[string(classifier.Coding)] = true
	}
	return active
}

// docgenCategories are the classifier categories for which a user asking to
// produce a downloadable document/spreadsheet/presentation is plausible. Other
// categories only get the file tools via keyword escalation. The FileToolGuardrail
// already stops the model from creating files unprompted, so this list can stay
// generous without spurious artifacts.
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

// docgenKeywords escalate the file-generation tools on. Bilingual (English +
// Swiss German, no ß) so a German request ("erstelle eine Präsentation",
// "exportier das als Tabelle") gates the same as its English equivalent. Bare
// "file"/"datei" are intentionally excluded — too common in unrelated contexts —
// while the specific artifact/format words are unambiguous.
var docgenKeywords = []string{
	// formats / extensions (language-neutral)
	"pdf", "docx", "xlsx", "pptx", "csv",
	// document
	"document", "dokument",
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

// codingKeywords escalate coding-relevant MCP tools (e.g. context7 docs) on when
// a turn shows coding intent regardless of the sticky category. Kept to strong
// signals to avoid over-triggering; the base "coding" category already covers
// threads that start as coding. Bilingual where a German dev would differ; most
// signals (code fences, package managers, tool names) are language-neutral.
var codingKeywords = []string{
	"```", // fenced code block
	"stack trace", "stacktrace", "traceback", "syntax error", "syntaxfehler",
	"exception", "segfault", "nullpointer",
	"npm ", "pnpm", "yarn ", "pip install", "cargo ", "go get", "gradle", "maven",
	"kubectl", "docker ", "git commit", "git push",
	"compile", "kompilier",
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
