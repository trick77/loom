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
// still small enough to keep the helper call cheap. Sized so a full user memory
// (well under 2000 characters ≈ ~500 tokens) is never clipped mid-output.
const memoryMaxCompletionTokens = 768

// ProjectMemorySystemPrompt drives project-memory generation: a sectioned
// profile shared across the project's chats, with five fixed markdown headings
// (Purpose & context, Current state, Key learnings & principles, Approach &
// patterns, Tools & resources). It prioritizes still-relevant facts and actively
// prunes resolved ones — the "Current state" section churns — so the memory
// stays structured, current, and bounded.
const ProjectMemorySystemPrompt = "You maintain a compact, durable memory profile for a chat project so separate chats share full context. " +
	"Given the project name, description, the existing memory, and recent conversation, output an UPDATED memory in exactly these five sections, each a markdown heading followed by terse '- ' fragment lines. " +
	"Omit a heading entirely if it has no items.\n" +
	"## Purpose & context — who the user is in this project and what they aim to accomplish: role/org, the goal, the domain, and the hard constraints they work under (regulatory, technical, organizational). " +
	"Durable: keep once captured; only replace with newer values.\n" +
	"## Current state — what is in progress now: active evaluations, decisions in flight, what stage things are at. " +
	"This section CHURNS: drop items that are resolved, shipped, superseded, or have not come up again.\n" +
	"## Key learnings & principles — durable insights, established facts, and principles that should guide future work; things the user decided, corrected, or insisted on. " +
	"Keep what still stands; drop what's overturned.\n" +
	"## Approach & patterns — how the user likes to work: preferred formats, what they value, how they reason and decide.\n" +
	"## Tools & resources — concrete systems, libraries, platforms, datasets, and references in use.\n" +
	"Prioritize what is still in force: hard constraints, standing decisions, open questions. " +
	"You are curating, not accumulating — actively drop items that are resolved, superseded, or no longer relevant. " +
	"Preserve specific names, numbers, dates, and terminology. Replace outdated facts with their newest value instead of listing both. " +
	"Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. " +
	"Write in a terse, telegraphic \"caveman\" style: keep only load-bearing words; drop articles (a/the), pronouns, and linking verbs (e.g. \"Chose Postgres over Mongo; deadline March 1\"). " +
	"Keep the whole memory well under 2000 characters. Output ONLY the memory content, no preamble."

// UserMemorySystemPrompt drives user-memory generation: a structured digest with
// a protected identity Core, a capped churning "Current focus" section, and a
// "Style" section capturing how the user wants to be answered — so durable
// identity facts are never lost, transient work ages out instead of piling up,
// and learned response preferences steer future replies.
const UserMemorySystemPrompt = "You maintain a compact, durable memory about the user so the assistant can stay personalized across all of their chats. " +
	"Given the existing memory and recent conversation, output an UPDATED memory in exactly these three sections, each a markdown heading followed by terse '- ' fragment lines:\n" +
	"## Core — durable identity facts the user revealed about THEMSELVES: where they live, employer/role, languages, hobbies and favourite things (places, games, food, media, and the like), strong dislikes (things they hate or loathe), and lasting preferences. " +
	"These are high priority: once captured, KEEP them — only replace a Core fact with its newer value; never drop one to save space.\n" +
	"## Current focus — at most 10 items: the user's active projects, ongoing goals, and current work. " +
	"This section CHURNS: when it is full and a new item appears, drop the oldest or least-active one; also drop items that look finished, superseded, or have not come up in recent conversation. Never let it grow past the 10-item cap.\n" +
	"## Style — how the user wants the assistant to respond, inferred from their feedback and reactions: preferred answer length, level of detail, tone, format, and language. " +
	"Capture a preference only when the user signals it — an explicit request or a repeated reaction (e.g. complaining answers are too long → \"prefers concise answers\"; switching to another language → records that language). Replace a preference with its newer value when the signals change.\n" +
	"Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. " +
	"Write in a terse, telegraphic \"caveman\" style: keep only load-bearing words; drop articles (a/the), pronouns, and linking verbs (e.g. \"Lives Zurich; backend dev at Acme\"). " +
	"Drop chit-chat, one-off task details, and anything about other people. Replace outdated facts with their newest value instead of listing both. " +
	"Omit a heading entirely if it has no items. " +
	"NEVER store passwords, API keys, secrets, payment details, or other sensitive credentials. " +
	"Keep the whole memory well under 2000 characters. Output ONLY the memory content, no preamble."

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
