package mcpserver

import "strings"

// OneLine collapses s's whitespace to single spaces and cuts it to at most n
// runes, the last one an ellipsis when it was cut: a description fit for one
// line of tool text. n below 1 gives "".
func OneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
