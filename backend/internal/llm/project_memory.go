package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// memoryMaxCompletionTokens is the answer budget for the memory helpers
// (llmwire adds the model's reasoning allowance on top): project memory, its
// edits, and the user-fact op lists and merges. A truncated reply is refused,
// never stored; a fact batch whose ops do not fit is halved and retried.
const memoryMaxCompletionTokens = 4000

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

// GenerateMemory re-summarizes a text memory (project memory). It is given a
// header block (the project's name/description), the prior memory and a
// transcript of recent messages, and returns a fresh, compact memory
// (re-summarize, not append) so the result stays bounded. The systemPrompt
// selects the memory's style. It takes plain strings to avoid a dependency on
// the chat package.
func (c *Client) GenerateMemory(ctx context.Context, header, priorMemory, transcript, systemPrompt, responseLanguage string) (string, error) {
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
	b.WriteString("\n\"\"\"\n")
	b.WriteString("\nUpdated memory:")

	messages := []Message{
		{Role: "system", Content: appendLanguageDirective(systemPrompt, responseLanguage)},
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
func (c *Client) ApplyMemoryEdit(ctx context.Context, header, currentMemory, instruction, styleSystemPrompt, responseLanguage string) (string, error) {
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
		{Role: "system", Content: appendLanguageDirective(styleSystemPrompt, responseLanguage)},
		{Role: "user", Content: b.String()},
	}
	return c.runMemoryCompletion(ctx, start, messages)
}

// ErrMemoryTruncated reports a memory reply cut off by the token cap. Callers
// must keep the prior memory: a memory's tail is its oldest context, and a
// truncated rewrite stored as-is silently erased it.
var ErrMemoryTruncated = errors.New("memory completion truncated")

// runMemoryCompletion executes a bounded chat completion for the memory helpers
// and returns the trimmed assistant content. A truncated reply is an error,
// never salvaged: a partial memory is worse than the old one.
func (c *Client) runMemoryCompletion(ctx context.Context, _ time.Time, messages []Message) (string, error) {
	reply, err := c.prose(ctx, messages, memoryMaxCompletionTokens)
	if err != nil {
		return "", err
	}
	if reply.Empty {
		return "", fmt.Errorf("memory completion returned no choices")
	}
	if reply.FinishReason == "length" {
		return "", fmt.Errorf("%w: completion_tokens=%d reasoning_tokens=%d answer_cap=%d",
			ErrMemoryTruncated, reply.Usage.CompletionTokens, reply.Usage.CompletionTokenDetails.ReasoningTokens, memoryMaxCompletionTokens)
	}
	return strings.TrimSpace(reply.Content), nil
}
