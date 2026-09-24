package httpapi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Every cut in this package lands on a rune boundary, whichever helper made it.
func TestTruncateHelpersNeverSplitRunes(t *testing.T) {
	euro := strings.Repeat("€", 500) // 3 bytes each: any byte cut not on a multiple of 3 splits a rune
	cases := map[string]string{
		"head":            truncateBytesOnRuneBoundary(euro, 100),
		"tail":            truncateTailToBytes(euro, 100),
		"snippetFromText": snippetFromText(euro),
		"capToolOutput":   capToolOutput(strings.Repeat("€", maxToolResultContentBytes)),
		"summarizeForLog": summarizeForLog(euro),
		"truncateHead":    truncateHead(euro, 100),
		"snippet":         snippet(euro),
	}
	for name, got := range cases {
		if !utf8.ValidString(got) {
			t.Errorf("%s produced invalid UTF-8: %q", name, got)
		}
	}
	if got := truncateBytesOnRuneBoundary(euro, 100); len(got) != 99 {
		t.Errorf("head cut = %d bytes, want 99 (33 whole runes)", len(got))
	}
	if got := truncateTailToBytes("abc"+euro, 100); !strings.HasPrefix(got, truncationEllipsis) || len(got) > 100 || strings.Contains(got, "abc") {
		t.Errorf("tail cut = %q (%d bytes), want the ellipsis plus the tail within 100 bytes", got, len(got))
	}
	if got := truncateTailToBytes("short", 100); got != "short" {
		t.Errorf("tail cut of a short string = %q, want unchanged", got)
	}
}
