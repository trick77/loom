package sidecar

import (
	"fmt"
	"strings"
)

// MinTokenLen is the floor for a token loom and a sidecar share.
const MinTokenLen = 32

// TokenProblem says what is wrong with a shared token, named by its variable,
// or "" when it is fine. loom and the sidecar check with this one rule: a
// token one side takes and the other refuses only loses the tool, silently.
func TokenProblem(name, token string) string {
	switch {
	case len(token) < MinTokenLen:
		return fmt.Sprintf("%s must be at least %d characters (openssl rand -hex 32)", name, MinTokenLen)
	case strings.ContainsFunc(token, func(c rune) bool { return c <= ' ' || c == 0x7f }):
		return name + " must not contain whitespace or control characters"
	}
	return ""
}
