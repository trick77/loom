package mcpserver

import "testing"

func TestOneLine(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"  a\n b\tc  ", 10, "a b c"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 4, "abc…"},
		{"ab cdef", 4, "ab…"}, // the space before the cut goes
		{"äöüéèà", 3, "äö…"},  // runes, not bytes
		{"abc", 1, "…"},
		{"abc", 0, ""},
		{"abc", -1, ""},
		{"", 0, ""},
	}
	for _, tc := range tests {
		if got := OneLine(tc.s, tc.n); got != tc.want {
			t.Errorf("OneLine(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
		}
	}
}
