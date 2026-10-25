// Package tokens sizes text in tokens without a tokenizer.
package tokens

// Estimate approximates the token count of s. Real BPE tokenization would
// require a runtime vocabulary download (tiktoken), which a self-hosted, offline
// deployment cannot rely on; ~4 characters/token is the standard heuristic and
// is sufficient for sizing chunks, the embedding budget and prompt blocks.
func Estimate(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}
