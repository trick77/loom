package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// memoryMaxCompletionTokens caps memory generation. A memory is a short markdown
// digest (a handful of lines), so this is far above a title's 32-token cap but
// still small enough to keep the helper call cheap.
const memoryMaxCompletionTokens = 512

// ProjectMemorySystemPrompt drives project-memory generation: a compact,
// topic-grouped digest shared across the project's chats.
const ProjectMemorySystemPrompt = "You maintain a compact, durable memory for a chat project so separate chats share context. " +
	"Given the project name, description, the existing memory, and recent conversation, output an UPDATED memory. " +
	"Keep ONLY durable, project-wide facts, decisions, and open questions (dates, budgets, people, hard constraints, choices made). " +
	"Drop chit-chat and one-off details. Replace outdated facts with their newest value instead of listing both. " +
	"Use short markdown bullet lines, grouped under brief headings if helpful. Be terse. " +
	"Stay well under 1000 characters. Output ONLY the memory content, no preamble."

// UserMemorySystemPrompt drives user-memory generation: a flat list of atomic,
// single-sentence facts about the user, injected into all of their chats.
const UserMemorySystemPrompt = "You maintain a compact, durable memory of facts about the user so the assistant can stay personalized across all of their chats. " +
	"Given the existing memory and recent conversation, output an UPDATED memory. " +
	"Keep ONLY durable, personal facts the user revealed about THEMSELVES (employer/role, location, languages, lasting preferences, recurring goals). " +
	"Each fact must be a single, self-contained sentence on its own line, prefixed with '- '. Do NOT group under headings. " +
	"Drop chit-chat, one-off details, and anything about other people. Replace outdated facts with their newest value instead of listing both. " +
	"NEVER store passwords, API keys, secrets, payment details, or other sensitive credentials. " +
	"Be terse. Stay well under 800 characters. Output ONLY the memory content, no preamble."

// GenerateMemory re-summarizes a memory. It is given a scope-specific header
// block (for example a project's name/description, or empty for user memory),
// the prior memory, and a transcript of recent messages, and returns a fresh,
// compact memory (re-summarize, not append) so the result stays bounded. The
// systemPrompt selects the memory's style (project vs. user). It takes plain
// strings to avoid a dependency on the chat package.
func (c *Client) GenerateMemory(ctx context.Context, header, priorMemory, transcript, systemPrompt string) (string, error) {
	start := time.Now()

	var b strings.Builder
	if h := strings.TrimSpace(header); h != "" {
		b.WriteString(h)
		b.WriteString("\n")
	}
	b.WriteString("\nExisting memory (may be empty):\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(priorMemory))
	b.WriteString("\n\"\"\"\n")
	b.WriteString("\nRecent conversation:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(transcript))
	b.WriteString("\n\"\"\"\n\nUpdated memory:")

	messages := []Message{
		{Role: "system", Content: systemPrompt},
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
		err := fmt.Errorf("decode memory completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return "", err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return "", fmt.Errorf("memory completion returned no choices")
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	return strings.TrimSpace(choice.Message.Content), nil
}
