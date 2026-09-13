package llm

import "time"

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type StreamEvent struct {
	Delta          string
	ReasoningDelta string
	ToolCall       ToolCall
	// ToolPending signals that the model has begun a tool call (inline marker
	// seen, or first native tool-call chunk) before the fully-parsed ToolCall is
	// emitted at the end of the turn. Lets the client keep showing "thinking"
	// instead of prematurely settling on a reasoning summary.
	ToolPending bool
}

type StreamResult struct {
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
	Usage            TokenUsage
	Duration         time.Duration
	Model            string
	ReasoningEffort  string
	FinishReason     string
	// CostNanoUSD is llmwire's list-rate figure for this call, in nano-USD,
	// and CostPriced says whether there was a rate at all. Unpriced is
	// unknown, not free: it stays out of every sum.
	CostNanoUSD int64
	CostPriced  bool
}

type TokenUsage struct {
	PromptTokens           int                    `json:"prompt_tokens"`
	CompletionTokens       int                    `json:"completion_tokens"`
	TotalTokens            int                    `json:"total_tokens"`
	PromptTokensDetails    PromptTokenDetails     `json:"prompt_tokens_details"`
	CompletionTokenDetails CompletionTokenDetails `json:"completion_tokens_details"`
}

type PromptTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type CompletionTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

func (u TokenUsage) Present() bool {
	return u.PromptTokens != 0 ||
		u.CompletionTokens != 0 ||
		u.TotalTokens != 0 ||
		u.PromptTokensDetails.CachedTokens != 0 ||
		u.CompletionTokenDetails.ReasoningTokens != 0
}
