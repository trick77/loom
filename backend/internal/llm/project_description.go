package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const projectDescriptionSystemPrompt = "You write concise project descriptions for a chat app. Given the project name and early conversation, reply with ONLY one neutral sentence fragment describing what the project is about. No markdown, no title, no quotes, no preamble. Keep it under 160 characters."

// projectDescriptionMaxCompletionTokens caps the description completion. A
// description is one ≤160-char sentence fragment (~40 tokens), but it now rides the
// project-memory refresh and is generated from the same large project transcript memory
// uses, so give it real output headroom (like memory's 768) — a tight title-sized cap
// truncates the reply to finish_reason=length, which is then salvaged (not discarded)
// below, but a generous cap avoids needing to salvage in the first place.
const projectDescriptionMaxCompletionTokens = 256

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
	resp, err := c.executeUtilityChatRequestWithBudget(ctx, messages, projectDescriptionMaxCompletionTokens)
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
		// Truncated mid-fragment. Salvage the partial text (dropping a dangling last
		// word) rather than discarding it: a clipped one-line description is far better
		// than a permanently empty one, and silently discarding is exactly what let the
		// empty-description bug persist unnoticed. Logged so a recurrence is visible.
		salvaged := salvageTruncatedDescription(choice.Message.Content)
		slog.Warn("project description truncated; salvaging partial",
			"completion_tokens", completion.Usage.CompletionTokens,
			"max_completion_tokens", projectDescriptionMaxCompletionTokens,
			"salvaged_empty", salvaged == "")
		return salvaged, nil
	}
	return cleanProjectDescription(choice.Message.Content), nil
}

// salvageTruncatedDescription turns a length-truncated completion into a usable
// description: drop the trailing (likely partial) word so it does not end mid-token,
// then clean. Keeps a lone word whole. Returns "" only when nothing usable remains.
func salvageTruncatedDescription(content string) string {
	content = strings.TrimSpace(content)
	if i := strings.LastIndexAny(content, " \t\n"); i > 0 {
		content = content[:i]
	}
	return cleanProjectDescription(content)
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
