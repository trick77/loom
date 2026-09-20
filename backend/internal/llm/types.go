package llm

import "time"

// Tool is a tool definition for model tool use, containing a function specification.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function specification within a tool, including its name, description, and parameters.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall is a model's invocation of a tool, identified by an ID and containing the tool call details.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the name and arguments of a tool call invoked by the model.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// StreamEvent is a single event emitted during a streamed model turn (text, reasoning, or tool call).
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

// StreamResult is the complete result of a streamed model turn, containing content, tool calls, and usage information.
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

// TokenUsage contains token counts for a model call (prompt, completion, cached, and reasoning tokens).
type TokenUsage struct {
	PromptTokens           int                    `json:"prompt_tokens"`
	CompletionTokens       int                    `json:"completion_tokens"`
	TotalTokens            int                    `json:"total_tokens"`
	PromptTokensDetails    PromptTokenDetails     `json:"prompt_tokens_details"`
	CompletionTokenDetails CompletionTokenDetails `json:"completion_tokens_details"`
}

// PromptTokenDetails contains detailed breakdown of prompt tokens including cached tokens.
type PromptTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// CompletionTokenDetails contains detailed breakdown of completion tokens including reasoning tokens.
type CompletionTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// Present reports whether the usage contains any non-zero token counts.
func (u TokenUsage) Present() bool {
	return u.PromptTokens != 0 ||
		u.CompletionTokens != 0 ||
		u.TotalTokens != 0 ||
		u.PromptTokensDetails.CachedTokens != 0 ||
		u.CompletionTokenDetails.ReasoningTokens != 0
}
