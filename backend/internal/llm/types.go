package llm

type chatCompletionRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	Stream          bool           `json:"stream"`
	Tools           []Tool         `json:"tools,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
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
	Content string `json:"content"`
}

type chatCompletionChunk struct {
	Choices []chatCompletionChunkChoice `json:"choices"`
	Usage   TokenUsage                  `json:"usage"`
}

type chatCompletionChunkChoice struct {
	Delta chatCompletionDelta `json:"delta"`
}

type chatCompletionDelta struct {
	Content   string               `json:"content"`
	ToolCalls []ToolCallDeltaChunk `json:"tool_calls"`
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
	Delta    string
	ToolCall ToolCall
}

type StreamResult struct {
	Content   string
	ToolCalls []ToolCall
	Usage     TokenUsage
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
