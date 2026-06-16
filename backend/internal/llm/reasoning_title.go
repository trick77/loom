package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/lume/internal/titletext"
)

const reasoningTitleSystemPrompt = "Summarize the assistant's private reasoning as a short present-participle (gerund) title naming the user's subject matter or the task being worked on. Use 3 to 8 words. Start with an -ing verb (e.g. \"Explaining what causes the northern lights\"). No first person, no sentences, no trailing punctuation. Name the topic the reasoning engages with, not the final answer. Never title the meta-process of reasoning — such as deciding whether tools are needed, choosing a response format, or judging the question's difficulty; name the underlying subject instead. Return only the title."

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
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return "", nil
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	// A title that hit the token cap is cut mid-phrase; skip it rather than
	// persist a half title (the caller falls back to the client-side heuristic).
	if choice.FinishReason == "length" {
		return "", nil
	}
	return cleanReasoningTitle(choice.Message.Content), nil
}

// cleanReasoningTitle strips surrounding quotes and caps the length. Unlike
// cleanChatTitle (tuned for thread titles), it never substitutes a placeholder: an
// empty or unusable result yields "" so the caller omits the title entirely.
func cleanReasoningTitle(title string) string {
	title = strings.TrimSpace(title)
	title = titletext.NormalizeQuotes(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.TrimSpace(titletext.StripWrappingQuotes(title))
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
