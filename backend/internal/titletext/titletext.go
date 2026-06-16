// Package titletext holds small string helpers shared by the chat- and
// reasoning-title cleanup paths. They live here, rather than in the llm or chat
// packages, so both can reuse them without an import cycle.
package titletext

import "strings"

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
