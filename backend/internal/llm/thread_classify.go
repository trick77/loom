package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/classifier"
)

// threadClassifySystemPrompt asks the helper model to pick exactly one category
// for the conversation. It is deliberately separate from the title call so the
// title prompt stays untouched and this prompt can be tuned purely for routing
// accuracy. The reply is a single category value (a few tokens), not JSON.
var threadClassifySystemPrompt = "You classify the first user message of a conversation into exactly one category. Reply with ONLY the category value (lowercase, no punctuation, nothing else). If none clearly fits, reply \"general\". Categories:\n" + classifier.PromptGuide()

// ClassifyThread picks the prompt-classifier category for a conversation from its
// first user message. It always returns a valid category — General on any request,
// decode, or empty-reply failure — so callers can use the result unconditionally;
// the returned error is informational (for logging) only.
func (c *Client) ClassifyThread(ctx context.Context, userMessage string) (string, error) {
	start := time.Now()
	framed := "First user message:\n\"\"\"\n" + strings.TrimSpace(userMessage) + "\n\"\"\"\n\nCategory:"
	messages := []Message{
		{Role: "system", Content: threadClassifySystemPrompt},
		{Role: "user", Content: framed},
	}
	resp, err := c.executeUtilityChatRequest(ctx, messages)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return string(classifier.General), err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode classify completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return string(classifier.General), err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return string(classifier.General), nil
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	// Match tolerantly extracts the category from the reply (handling quotes,
	// punctuation, or stray prose) and coerces anything unrecognized — including a
	// truncated "length" reply — to General, so a bad reply never produces a bad
	// category.
	return string(classifier.Match(choice.Message.Content)), nil
}
