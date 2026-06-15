package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const projectDescriptionSystemPrompt = "You write concise project descriptions for a chat app. Given the project name and early conversation, reply with ONLY one neutral sentence fragment describing what the project is about. No markdown, no title, no quotes, no preamble. Keep it under 160 characters."

func (c *Client) GenerateProjectDescription(ctx context.Context, projectName, transcript string) (string, error) {
	start := time.Now()
	var b strings.Builder
	b.WriteString("Project name:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(projectName))
	b.WriteString("\n\"\"\"\n\nEarly conversation:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(transcript))
	b.WriteString("\n\"\"\"\n\nProject description:")

	messages := []Message{
		{Role: "system", Content: projectDescriptionSystemPrompt},
		{Role: "user", Content: b.String()},
	}
	resp, err := c.executeUtilityChatRequest(ctx, messages)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode project description completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return "", fmt.Errorf("project description completion returned no choices")
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	if choice.FinishReason == "length" {
		return "", nil
	}
	return cleanProjectDescription(choice.Message.Content), nil
}

func cleanProjectDescription(description string) string {
	description = strings.TrimSpace(description)
	description = strings.Trim(description, `"'`)
	description = strings.TrimSpace(description)
	description = strings.TrimRight(description, ".")
	description = strings.TrimSpace(description)
	if description != "" && !strings.HasSuffix(description, "!") && !strings.HasSuffix(description, "?") {
		description += "."
	}
	return description
}
