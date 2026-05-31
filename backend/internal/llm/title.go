package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const titleSystemPrompt = "Name this chat in 2 to 6 words. Return only the title."

func (c *Client) GenerateTitle(ctx context.Context, userMessage, assistantMessage string) (string, error) {
	resp, err := c.executeChatRequest(ctx, []Message{
		{Role: "system", Content: titleSystemPrompt},
		{Role: "user", Content: userMessage},
		{Role: "assistant", Content: assistantMessage},
	}, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return "", fmt.Errorf("decode title completion response: %w", err)
	}
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

	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80])
	}
	return title
}
