package llm

import "time"

type chatCompletionRequest struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	Stream          bool            `json:"stream"`
	Tools           []Tool          `json:"tools,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Thinking        *thinkingOption `json:"thinking,omitempty"`
	StreamOptions   *streamOptions  `json:"stream_options,omitempty"`
}

// thinkingOption is MiMo's native switch for chain-of-thought. {"type":"disabled"}
// turns thinking off entirely (no reasoning_content, zero reasoning tokens).
type thinkingOption struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
	Usage   TokenUsage             `json:"usage"`
}

type chatCompletionChoice struct {
	Message chatCompletionMessage `json:"message"`
}

type chatCompletionMessage struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type chatCompletionChunk struct {
	Choices []chatCompletionChunkChoice `json:"choices"`
	Usage   TokenUsage                  `json:"usage"`
}

type chatCompletionChunkChoice struct {
	Delta chatCompletionDelta `json:"delta"`
}

type chatCompletionDelta struct {
	Content          string               `json:"content"`
	ReasoningContent string               `json:"reasoning_content"`
	ToolCalls        []ToolCallDeltaChunk `json:"tool_calls"`
}

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

type ToolCallDeltaChunk struct {
	Index    int               `json:"index"`
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function ToolCallDeltaFunc `json:"function"`
}

type ToolCallDeltaFunc struct {
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
