package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const reasoningTitleSystemPrompt = "Summarize the assistant's private reasoning as a short present-participle (gerund) title. Use 3 to 8 words. Start with an -ing verb (e.g. \"Debugging syntax errors in frameworks array\"). No first person, no sentences, no trailing punctuation. Describe what the reasoning is doing, not its conclusion. Return only the title."

// GenerateReasoningTitle produces a short gerund-style abstract for one round of
// model reasoning. It is a secondary, non-streaming call meant to run in the
// background. On any failure or an unusable result it returns an empty string so
// the caller simply skips the title rather than surfacing an error.
func (c *Client) GenerateReasoningTitle(ctx context.Context, reasoning string) (string, error) {
	if strings.TrimSpace(reasoning) == "" {
		return "", nil
	}
	start := time.Now()
	messages := []Message{
		{Role: "system", Content: reasoningTitleSystemPrompt},
		{Role: "user", Content: reasoning},
	}
	resp, err := c.executeUtilityChatRequest(ctx, messages)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode reasoning title completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	logInferenceCompleted(ctx, c.model, time.Since(start), completion.Usage)
	if len(completion.Choices) == 0 {
		return "", nil
	}
	return cleanReasoningTitle(completion.Choices[0].Message.Content), nil
}

// cleanReasoningTitle strips surrounding quotes and caps the length. Unlike
// cleanChatTitle (tuned for thread titles), it never substitutes a placeholder: an
// empty or unusable result yields "" so the caller omits the title entirely.
func cleanReasoningTitle(title string) string {
	title = strings.TrimSpace(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.TrimSpace(strings.Trim(title, `"'`))
	}
	title = trimTrailingDots(title)
	if title == "" {
		return ""
	}
	if runes := []rune(title); len(runes) > 80 {
		title = string(runes[:80])
	}
	return title
}
