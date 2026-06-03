package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const titleSystemPrompt = "Name this chat topic in 2 to 6 words. Do not answer the user. If unsure, use the user's own subject words. Return only the title."

func (c *Client) GenerateTitle(ctx context.Context, userMessage, assistantMessage string) (string, error) {
	start := time.Now()
	messages := []Message{
		{Role: "system", Content: titleSystemPrompt},
		{Role: "user", Content: userMessage},
	}
	// Only include the assistant turn when it has content. An assistant message
	// with empty content serializes to {"role":"assistant"} (content is
	// omitempty), which providers reject with "assistant must provide content".
	if strings.TrimSpace(assistantMessage) != "" {
		messages = append(messages, Message{Role: "assistant", Content: assistantMessage})
	}
	resp, err := c.executeChatRequest(ctx, messages, false)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode title completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	logInferenceCompleted(ctx, c.model, time.Since(start), completion.Usage)
	if len(completion.Choices) == 0 {
		return "New chat", nil
	}
	return cleanTitle(completion.Choices[0].Message.Content), nil
}

func cleanTitle(title string) string {
	title = strings.TrimSpace(title)
	if unquoted, err := strconv.Unquote(title); err == nil {
		title = strings.TrimSpace(unquoted)
	} else {
		title = strings.Trim(title, `"'`)
		title = strings.TrimSpace(title)
	}
	if title == "" {
		return "New chat"
	}
	if isAnswerLikeTitle(title) {
		return "New chat"
	}

	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80])
	}
	return title
}

func isAnswerLikeTitle(title string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(title), " "))
	answerPrefixes := []string{
		"i don't have ",
		"i do not have ",
		"i don't know",
		"i do not know",
		"i'm sorry",
		"i am sorry",
		"sorry,",
		"as an ai",
		"i can't ",
		"i cannot ",
	}
	for _, prefix := range answerPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}
