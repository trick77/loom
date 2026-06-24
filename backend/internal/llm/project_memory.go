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
	"Always preserve the durable facts, decisions, and constraints already captured — never drop a captured durable fact merely to save space; compress the wording, not the facts. " +
	"Use terse fragments in short markdown bullet lines, grouped under brief headings if helpful. " +
	"Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. " +
	"Stay well under 1000 characters (this governs phrasing, not whether to keep a captured durable fact). Output ONLY the memory content, no preamble."

// UserMemorySystemPrompt drives user-memory generation: a flat list of atomic,
// single-sentence facts about the user, injected into all of their chats.
const UserMemorySystemPrompt = "You maintain a compact, durable memory of facts about the user so the assistant can stay personalized across all of their chats. " +
	"Given the existing memory and recent conversation, output an UPDATED memory. " +
	"Keep ONLY durable, personal facts the user revealed about THEMSELVES (employer/role, location, languages, lasting preferences, recurring goals). " +
	"Always preserve durable, identity-defining facts already captured — location, languages, hobbies, favourite things (places, games, food, media, and the like), strong dislikes (things the user hates or loathes), lasting preferences, and recurring goals — and never drop a captured durable fact merely to save space; compress the wording, not the facts. " +
	"Use terse fragments: each fact must be a fragment on its own line, prefixed with '- '. Do NOT group under headings. " +
	"Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. " +
	"Drop chit-chat, one-off details, and anything about other people. Replace outdated facts with their newest value instead of listing both. " +
	"NEVER store passwords, API keys, secrets, payment details, or other sensitive credentials. " +
	"Stay well under 800 characters (this governs phrasing, not whether to keep a captured durable fact). Output ONLY the memory content, no preamble."

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
	return c.runMemoryCompletion(ctx, start, messages)
}

// ApplyMemoryEdit applies a single natural-language instruction to an existing
// memory in place — adding, modifying, or removing facts as asked — and returns
// the full updated memory. Unlike GenerateMemory it does not re-summarize a
// transcript; it edits the current content directly. The instruction is
// authoritative: a request to remove a fact is honored even when that fact would
// normally be a protected, durable one. The styleSystemPrompt (the scope's
// project/user prompt) keeps the output's format consistent. An empty result is
// valid (the user emptied the memory).
func (c *Client) ApplyMemoryEdit(ctx context.Context, header, currentMemory, instruction, styleSystemPrompt string) (string, error) {
	start := time.Now()

	var b strings.Builder
	if h := strings.TrimSpace(header); h != "" {
		b.WriteString(h)
		b.WriteString("\n")
	}
	b.WriteString("\nYou are editing the user's existing memory. Apply ONLY the instruction below, leaving every other fact unchanged, and output the full updated memory in the same format and style. ")
	b.WriteString("The instruction is authoritative: if it asks to remove or change a fact, do so even if that fact is normally durable or protected. ")
	b.WriteString("If the instruction empties the memory, output nothing.\n")
	b.WriteString("\nExisting memory (may be empty):\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(currentMemory))
	b.WriteString("\n\"\"\"\n")
	b.WriteString("\nInstruction:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(instruction))
	b.WriteString("\n\"\"\"\n\nUpdated memory:")

	messages := []Message{
		{Role: "system", Content: styleSystemPrompt},
		{Role: "user", Content: b.String()},
	}
	return c.runMemoryCompletion(ctx, start, messages)
}

// runMemoryCompletion executes a bounded, non-thinking chat completion for the
// memory helpers and returns the trimmed assistant content.
func (c *Client) runMemoryCompletion(ctx context.Context, start time.Time, messages []Message) (string, error) {
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
