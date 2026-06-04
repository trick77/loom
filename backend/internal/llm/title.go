package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const titleSystemPrompt = "Write a chat title as a neutral noun phrase. Use 2 to 6 words. Do not write a sentence. Do not use first person, second person, future tense, promises, or assistant actions. Prefer subject titles like \"Photorealistic cat image\" over action titles like \"I'll create a cat image\". Return only the title."

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
	title = rewriteFirstPersonCreationTitle(title)
	if isAnswerLikeTitle(title) {
		return "New chat"
	}

	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80])
	}
	return title
}

func rewriteFirstPersonCreationTitle(title string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(title), " "))
	prefixes := []string{
		"i'll create ",
		"i will create ",
		"i'll generate ",
		"i will generate ",
		"i'll make ",
		"i will make ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			subject := strings.TrimSpace(title[len(prefix):])
			subject = strings.TrimSuffix(subject, ".")
			subject = strings.TrimSuffix(subject, "!")
			subject = strings.TrimSpace(subject)
			subject = strings.TrimSuffix(subject, " for you")
			subject = strings.TrimSuffix(subject, " for me")
			subject = strings.TrimSpace(subject)
			if subject != "" {
				return "Creation of " + subject
			}
		}
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
