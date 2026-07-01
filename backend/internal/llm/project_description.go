package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const projectDescriptionSystemPrompt = "You write one concise big-picture description of a project for a chat app. You are given the project name and the titles of every conversation thread in the project. Reply with ONLY one neutral sentence fragment that captures the overall theme tying the threads together — describe the project as a whole, do not list or enumerate the threads. No markdown, no title, no quotes, no preamble. Hard limit: one sentence, at most 160 characters."

// projectDescriptionMaxCompletionTokens caps the description completion. A
// description is one ≤160-char sentence fragment (~40 tokens); give it some output
// headroom so a slightly verbose reply finishes cleanly rather than hitting
// finish_reason=length and needing salvage. The final text is hard-capped to
// projectDescriptionMaxChars below regardless.
const projectDescriptionMaxCompletionTokens = 256

// projectDescriptionMaxChars hard-caps the final description length (runes). The
// prompt already asks for ≤160 chars, but this is enforced in code on a word boundary
// so a model that ignores the instruction (or a length-truncated salvage) can never
// produce an oversized description. One rune is reserved for the trailing period.
const projectDescriptionMaxChars = 160

// GenerateProjectDescription summarizes the project's thread titles into a single
// big-picture sentence fragment describing the project as a whole. titles are the
// meaningfully-titled, non-archived threads in the project (placeholder "New thread"
// titles excluded by the caller).
func (c *Client) GenerateProjectDescription(ctx context.Context, projectName string, titles []string, responseLanguage string) (string, error) {
	start := time.Now()
	var b strings.Builder
	b.WriteString("Project name:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(projectName))
	b.WriteString("\n\"\"\"\n\nThread titles in this project:\n\"\"\"\n")
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(title)
		b.WriteString("\n")
	}
	b.WriteString("\"\"\"\n\nProject description:")

	messages := []Message{
		{Role: "system", Content: appendLanguageDirective(projectDescriptionSystemPrompt, responseLanguage)},
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
	// Enforce the hard length cap on a word boundary. Reserve one rune for the
	// trailing period appended below so the final string never exceeds the cap.
	description = truncateToMaxChars(description, projectDescriptionMaxChars-1)
	if description != "" && !strings.HasSuffix(description, "!") && !strings.HasSuffix(description, "?") {
		description += "."
	}
	return description
}

// truncateToMaxChars caps content to max runes, cutting at the last whitespace before
// the limit so it never ends mid-word (falling back to a hard rune cut when a single
// word already exceeds max). Trailing punctuation/space is trimmed.
func truncateToMaxChars(content string, max int) string {
	runes := []rune(content)
	if len(runes) <= max {
		return content
	}
	truncated := string(runes[:max])
	if i := strings.LastIndexAny(truncated, " \t\n"); i > 0 {
		truncated = truncated[:i]
	}
	truncated = strings.TrimRight(truncated, " \t\n,;:-")
	return strings.TrimSpace(truncated)
}
