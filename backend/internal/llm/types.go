package llm

type chatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	Message chatCompletionMessage `json:"message"`
}

type chatCompletionMessage struct {
	Content string `json:"content"`
}

type chatCompletionChunk struct {
	Choices []chatCompletionChunkChoice `json:"choices"`
}

type chatCompletionChunkChoice struct {
	Delta chatCompletionDelta `json:"delta"`
}

type chatCompletionDelta struct {
	Content string `json:"content"`
}
