package httpapi

import "unicode/utf8"

// truncationEllipsis marks a cut in text shown to the model or the user.
const truncationEllipsis = "…"

// truncateBytesOnRuneBoundary keeps at most max bytes from the start of s,
// never splitting a multi-byte character. It is the one primitive every
// head-side cut in this package builds on; the byte-slice-and-hope versions
// it replaces each handled the boundary differently, one of them wrongly.
func truncateBytesOnRuneBoundary(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// truncateTailToBytes keeps the END of s within byteBudget bytes, prefixed
// with an ellipsis, cutting on a rune boundary: the conversation tail is what
// a digest wants, not its opening.
func truncateTailToBytes(s string, byteBudget int) string {
	if len(s) <= byteBudget {
		return s
	}
	avail := max(byteBudget-len(truncationEllipsis), 0)
	start := len(s) - avail
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return truncationEllipsis + s[start:]
}
