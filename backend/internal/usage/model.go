package usage

// Totals is a user's lifetime usage. JSON tags match the frontend Usage type.
//
// Table: user_usage_totals
type Totals struct {
	PromptTokens      int `json:"promptTokens"`
	CompletionTokens  int `json:"completionTokens"`
	CachedTokens      int `json:"cachedTokens"`
	ReasoningTokens   int `json:"reasoningTokens"`
	TotalTokens       int `json:"totalTokens"`
	EmbeddingTokens   int `json:"embeddingTokens"`
	EmbeddingRequests int `json:"embeddingRequests"`
	WebSearches       int `json:"webSearches"`
	WebFetches        int `json:"webFetches"`
	ObscuraFetches    int `json:"obscuraFetches"`
	ImageGens         int `json:"imageGens"`
	CodeRuns          int `json:"codeRuns"`
	// CostNanoUSD is the lifetime list-rate cost across chat and embedding
	// calls, priced calls only.
	CostNanoUSD     int64 `json:"costNanoUsd"`
	ThreadsCreated  int   `json:"threadsCreated"`
	ProjectsCreated int   `json:"projectsCreated"`
}
