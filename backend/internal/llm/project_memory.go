package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// memoryMaxCompletionTokens caps project-memory generation. A memory is a short
// markdown digest (a handful of lines), so this is far above a title's 32-token
// cap but still small enough to keep the helper call cheap.
const memoryMaxCompletionTokens = 512

const projectMemorySystemPrompt = "You maintain a compact, durable memory for a chat project so separate chats share context. " +
	"Given the project name, description, the existing memory, and recent conversation, output an UPDATED memory. " +
	"Keep ONLY durable, project-wide facts, decisions, and open questions (dates, budgets, people, hard constraints, choices made). " +
	"Drop chit-chat and one-off details. Replace outdated facts with their newest value instead of listing both. " +
	"Use short markdown bullet lines, grouped under brief headings if helpful. Be terse. " +
	"Stay well under 1000 characters. Output ONLY the memory content, no preamble."

// GenerateProjectMemory re-summarizes a project's memory. It is given the prior
// memory plus a transcript of recent messages and returns a fresh, compact
// memory (re-summarize, not append) so the result stays bounded. It takes plain
// strings to avoid a dependency on the chat package.
func (c *Client) GenerateProjectMemory(ctx context.Context, projectName, projectDescription, priorMemory, transcript string) (string, error) {
	start := time.Now()

	var b strings.Builder
	b.WriteString("Project name:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(projectName))
	b.WriteString("\n\"\"\"\n")
	if strings.TrimSpace(projectDescription) != "" {
		b.WriteString("\nProject description:\n\"\"\"\n")
		b.WriteString(strings.TrimSpace(projectDescription))
		b.WriteString("\n\"\"\"\n")
	}
	b.WriteString("\nExisting memory (may be empty):\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(priorMemory))
	b.WriteString("\n\"\"\"\n")
	b.WriteString("\nRecent conversation:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(transcript))
	b.WriteString("\n\"\"\"\n\nUpdated memory:")

	messages := []Message{
		{Role: "system", Content: projectMemorySystemPrompt},
		{Role: "user", Content: b.String()},
	}
	resp, err := c.executeChatRequestImpl(ctx, messages, chatRequestOptions{
		thinking:            &thinkingOption{Type: "disabled"},
		maxCompletionTokens: memoryMaxCompletionTokens,
	})
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode project memory completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return "", fmt.Errorf("project memory completion returned no choices")
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	return strings.TrimSpace(choice.Message.Content), nil
}
