package turn

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncatePrimitivesCutOnRuneBoundaries(t *testing.T) {
	euro := strings.Repeat("€", 500) // 3 bytes each: any byte cut not on a multiple of 3 splits a rune
	if got := TruncateBytesOnRuneBoundary(euro, 100); len(got) != 99 || !utf8.ValidString(got) {
		t.Errorf("head cut = %d bytes, want 99 (33 whole runes)", len(got))
	}
	if got := TruncateBytesOnRuneBoundary(euro, 0); got != "" {
		t.Errorf("head cut to 0 bytes = %q, want empty", got)
	}
	if got := TruncateBytesOnRuneBoundary("short", 100); got != "short" {
		t.Errorf("head cut of a short string = %q, want unchanged", got)
	}
	if got := TruncateTailToBytes("abc"+euro, 100); !strings.HasPrefix(got, TruncationEllipsis) || len(got) > 100 || strings.Contains(got, "abc") || !utf8.ValidString(got) {
		t.Errorf("tail cut = %q (%d bytes), want the ellipsis plus the tail within 100 bytes", got, len(got))
	}
	if got := TruncateTailToBytes("short", 100); got != "short" {
		t.Errorf("tail cut of a short string = %q, want unchanged", got)
	}
}
