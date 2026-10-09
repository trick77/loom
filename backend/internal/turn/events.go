package turn

// StreamDeltaResponse is the payload of assistant_delta and
// assistant_reasoning_delta: one streamed chunk of answer or reasoning text.
type StreamDeltaResponse struct {
	Content string `json:"content"`
}

// ToolCallResponse is the payload of tool_call: a tool call the model made.
type ToolCallResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultResponse is the payload of tool_result: what a tool call returned
// to the model.
type ToolResultResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// WebSourcesResponse carries the full set of web sources gathered so far in the
// turn, re-sent after every tool round so the browser can resolve inline [n]
// markers while the answer is still streaming. Mirrors the knowledge_sources
// event's shape.
type WebSourcesResponse struct {
	Sources []Citation `json:"sources"`
}
