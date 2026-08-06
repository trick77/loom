// Package titletext holds small string helpers shared by the chat- and
// reasoning-title cleanup paths. They live here, rather than in the llm or chat
// packages, so both can reuse them without an import cycle.
package titletext

import (
	"strings"
	"unicode"
)

// typographicQuoteReplacer maps the curly quotes that title models routinely
// emit onto their ASCII equivalents so downstream cleanup (Unquote, balanced
// stripping, prefix matching) only ever has to reason about straight quotes.
var typographicQuoteReplacer = strings.NewReplacer(
	"“", `"`, // “ left double quotation mark
	"”", `"`, // ” right double quotation mark
	"‘", "'", // ‘ left single quotation mark
	"’", "'", // ’ right single quotation mark
)

// NormalizeQuotes rewrites typographic double/single quotes as straight ASCII
// quotes, leaving everything else untouched.
func NormalizeQuotes(s string) string {
	return typographicQuoteReplacer.Replace(s)
}

// StripWrappingQuotes removes surrounding quotes only when the first and last
// rune form a matching pair (both " or both '), peeling nested pairs in a loop.
// A leading quote without a matching trailing one is left in place: stripping it
// unconditionally is what turned a title like `"Healing" by Evanescence` into a
// dangling `Healing" by Evanescence`.
func StripWrappingQuotes(s string) string {
	for {
		runes := []rune(s)
		if len(runes) < 2 {
			return s
		}
		first, last := runes[0], runes[len(runes)-1]
		if (first == '"' || first == '\'') && first == last {
			s = string(runes[1 : len(runes)-1])
			continue
		}
		return s
	}
}

// scriptGroup is one writing-system equivalence class. The tables are Unicode
// script metadata, not a language keyword list: they contain no words, are not
// maintained per language, and are only ever consulted on model OUTPUT — so this
// is not the kind of hand-maintained lexicon the input-steering gates avoid.
type scriptGroup struct {
	name   string
	tables []*unicode.RangeTable
}

// scriptGroups is deliberately an explicit slice rather than a walk of
// unicode.Scripts: that map holds ~170 tables (a binary search each, per rune)
// and its ideographic entries are not disjoint, so "first match wins" would be
// unsound. Listing the groups also makes the CJK grouping expressible at all —
// ordinary Japanese mixes Han, Hiragana and Katakana in one sentence, so
// splitting them would reject a Han-only title written from a kana source.
// Anything not listed here is treated as a script we do not recognize.
var scriptGroups = []scriptGroup{
	{"latin", []*unicode.RangeTable{unicode.Latin}},
	{"cjk", []*unicode.RangeTable{unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo}},
	{"cyrillic", []*unicode.RangeTable{unicode.Cyrillic}},
	{"greek", []*unicode.RangeTable{unicode.Greek}},
	{"arabic", []*unicode.RangeTable{unicode.Arabic}},
	{"hebrew", []*unicode.RangeTable{unicode.Hebrew}},
	{"devanagari", []*unicode.RangeTable{unicode.Devanagari}},
	{"thai", []*unicode.RangeTable{unicode.Thai}},
}

// scriptGroupOf returns the name of the group r belongs to, or "" when r is not
// a letter or belongs to no listed group.
func scriptGroupOf(r rune) string {
	if !unicode.IsLetter(r) {
		return ""
	}
	for _, group := range scriptGroups {
		if unicode.In(r, group.tables...) {
			return group.name
		}
	}
	return ""
}

// DriftsFromSourceScripts reports whether title uses a writing system that
// appears in none of the given source texts. It exists because the short-gate
// title model sometimes answers in Chinese regardless of the language directive
// in its system prompt; a title in a script the source never used is that drift,
// not a translation the user asked for.
//
// Callers must pass every text whose script is legitimately expected in the
// output. The requested response language is deliberately NOT one of them:
// callers hold its English display name ("German"), not its language tag, so
// passing it would only ever mean "allow Latin" — and would silently stop
// working the day a non-Latin language became selectable.
//
// Only letters carry script identity, so digits, punctuation, symbols and emoji
// are ignored. A letter in no recognized group never triggers drift: the guard
// fires only on scripts it positively identifies, so an unforeseen script is
// allowed through rather than guessed at. Passing no sources therefore rejects
// every recognized script, Latin included — callers must supply their source
// text rather than relying on an implicit default.
func DriftsFromSourceScripts(title string, sources ...string) bool {
	titleGroups := make(map[string]bool)
	for _, r := range title {
		if group := scriptGroupOf(r); group != "" {
			titleGroups[group] = true
		}
	}
	if len(titleGroups) == 0 {
		return false
	}
	for _, source := range sources {
		for _, r := range source {
			if group := scriptGroupOf(r); group != "" {
				delete(titleGroups, group)
			}
		}
		if len(titleGroups) == 0 {
			return false
		}
	}
	return len(titleGroups) > 0
}
