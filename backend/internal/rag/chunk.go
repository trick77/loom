// Package rag implements document retrieval-augmented generation: chunking
// extracted text, embedding chunks, storing them, and retrieving the most
// relevant chunks for a query.
package rag

import "strings"

// TextChunk is one unit of indexed text with its position and an estimated token count.
type TextChunk struct {
	Ordinal    int
	Text       string
	TokenCount int
}

// ChunkOptions controls chunk sizing. Sizes are in estimated tokens.
type ChunkOptions struct {
	// TargetTokens is the soft target size of a chunk.
	TargetTokens int
	// MaxTokens is the hard ceiling; no emitted chunk exceeds it.
	MaxTokens int
	// OverlapTokens is how much trailing content is repeated at the start of the
	// next chunk to preserve context across boundaries.
	OverlapTokens int
}

// DefaultChunkOptions targets ~600 tokens with ~12% overlap, matching the design
// (~500-800 tokens, 10-15% overlap) and AnythingLLM's chunk-with-overlap approach.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{TargetTokens: 600, MaxTokens: 800, OverlapTokens: 75}
}

// estimateTokens approximates the token count of s. Real BPE tokenization would
// require a runtime vocabulary download (tiktoken), which a self-hosted, offline
// deployment cannot rely on; ~4 characters/token is the standard heuristic and
// is sufficient for sizing chunks and the embedding budget.
func estimateTokens(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	t := (n + 3) / 4
	if t < 1 {
		t = 1
	}
	return t
}

// Chunk splits text into overlapping, token-bounded chunks on whitespace
// boundaries. Whitespace-only input yields no chunks.
func Chunk(text string, opts ChunkOptions) []TextChunk {
	if opts.TargetTokens <= 0 {
		opts = DefaultChunkOptions()
	}
	if opts.MaxTokens < opts.TargetTokens {
		opts.MaxTokens = opts.TargetTokens
	}
	if opts.OverlapTokens < 0 || opts.OverlapTokens >= opts.TargetTokens {
		opts.OverlapTokens = opts.TargetTokens / 8
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	// Average tokens per word for this text, used to translate the token budgets
	// into word counts (cheap, avoids re-estimating on every append).
	tokensPerWord := float64(estimateTokens(text)) / float64(len(words))
	if tokensPerWord <= 0 {
		tokensPerWord = 1
	}
	wordsPerTarget := max(1, int(float64(opts.TargetTokens)/tokensPerWord))
	wordsPerMax := max(wordsPerTarget, int(float64(opts.MaxTokens)/tokensPerWord))
	overlapWords := min(int(float64(opts.OverlapTokens)/tokensPerWord), wordsPerTarget-1)
	if overlapWords < 0 {
		overlapWords = 0
	}

	var chunks []TextChunk
	for start := 0; start < len(words); {
		end := min(start+wordsPerTarget, len(words))
		// Never exceed the hard max even if the target/overlap math drifts.
		if end-start > wordsPerMax {
			end = start + wordsPerMax
		}
		text := strings.Join(words[start:end], " ")
		chunks = append(chunks, TextChunk{
			Ordinal:    len(chunks),
			Text:       text,
			TokenCount: estimateTokens(text),
		})
		if end >= len(words) {
			break
		}
		// Advance, retaining `overlapWords` of trailing context. Guard against a
		// zero-progress step.
		next := end - overlapWords
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return chunks
}
