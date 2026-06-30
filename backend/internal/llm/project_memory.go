package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// memoryMaxCompletionTokens caps memory generation. A full user memory runs up to
// userMemoryBudgetChars (~6000 chars ≈ ~1500 tokens), so this is sized above that
// to never clip the output mid-section. It is a ceiling shared with project memory
// (bounded separately by its own prompt + MaxProjectMemoryLength), so the larger
// value does not enlarge project memory.
const memoryMaxCompletionTokens = 1900

// userMemoryBudgetChars is the total character budget the user-memory generation
// prompt divides across its sections. It is kept in lockstep with
// chat.MaxUserMemoryLength (asserted in a test) — llm deliberately does not import
// chat, so the value is mirrored here rather than referenced.
const userMemoryBudgetChars = 6000

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

// userMemorySection is one heading in the derived user memory. pct is the
// section's share of the total budget; the per-section character target is
// computed from it (budget*pct/100) so there are no hardcoded magic char counts —
// change userMemoryBudgetChars and every target rescales. The pct values sum to
// 100 (verified in a test).
type userMemorySection struct {
	heading string
	pct     int
	note    string
}

// userMemorySections defines the derived-memory structure and its proportional
// emphasis. The three "###" entries are the sub-sections of "## Brief history"
// (its umbrella heading is rendered separately). Time-layered: items demote from
// Top of mind → Recent months → Earlier context → Long-term background as they
// stop recurring.
var userMemorySections = []userMemorySection{
	{"## Work context", 12, "the user's professional life: employer/org, role, field, what they build or work on, tools and stack, and hard work constraints"},
	{"## Personal context", 11, "durable personal identity and TASTE facts: where they live, languages, family/relationships mentioned in passing, hobbies, and likes/dislikes as personal taste (food, media, places, activities) — NOT preferences about how the assistant should respond, which are the user's separate standing instructions"},
	{"## Top of mind", 13, "what the user is actively focused on right now: current projects, open goals, decisions in flight, recurring recent themes. CHURNS: drop items once they look finished, superseded, or stop coming up"},
	{"### Recent months", 36, "the richest, most detailed section: notable threads of work and life from roughly the last few months"},
	{"### Earlier context", 18, "older but still relevant background that has faded from active focus"},
	{"### Long-term background", 10, "durable, long-settled facts and milestones worth keeping indefinitely, compressed to essentials"},
}

// UserMemorySystemPrompt drives derived user-memory generation: a read-only,
// daily-regenerated digest organized into work/personal context, what's top of
// mind, and a time-layered brief history that ages items downward as they stop
// recurring. It is built from userMemorySections so the per-section char targets
// are computed from the budget, not hardcoded. The user steers behavior through
// the separate standing-instructions (directive) layer, not by editing this memory.
var UserMemorySystemPrompt = buildUserMemorySystemPrompt(userMemoryBudgetChars)

func buildUserMemorySystemPrompt(budget int) string {
	var b strings.Builder
	b.WriteString("You maintain a compact, durable memory about the user so the assistant can stay personalized across all of their chats. ")
	b.WriteString("Given the existing memory and recent conversation, output an UPDATED memory using exactly these markdown headings, each followed by terse '- ' fragment lines. Use these headings and no others, and omit any heading that would have no items:\n")
	briefHistoryWritten := false
	for _, s := range userMemorySections {
		if strings.HasPrefix(s.heading, "### ") && !briefHistoryWritten {
			b.WriteString("## Brief history — the user's story over time, most recent first, split into the three sub-sections below. As items age and stop recurring, DEMOTE them one level on each regeneration: Top of mind → Recent months → Earlier context → Long-term background (or drop when no longer worth keeping); promote back up only when an item becomes active again. You are given no real dates — judge recency by how prominently and recently each item appears in the conversation and prior memory, not by calendar math. Brief history as a whole should be the largest part of the memory.\n")
			briefHistoryWritten = true
		}
		fmt.Fprintf(&b, "%s — %s. Aim for roughly %d characters.\n", s.heading, s.note, budget*s.pct/100)
	}
	fmt.Fprintf(&b, "The character targets are SOFT proportions of the memory's budget, not hard limits: let stronger sections run longer and weaker ones shorter, but keep the WHOLE memory under %d characters.\n", budget)
	b.WriteString("If the existing memory uses OLD headings (## Core, ## Current focus, ## Style), re-bucket their content into the new sections: identity/employer/location/taste facts go to ## Work context or ## Personal context; active work goes to ## Top of mind or ### Recent months; long-settled facts go to ### Long-term background. Any ## Style response-behavior preferences belong to the user's separate standing instructions, NOT this memory — drop them here. Never carry an old heading forward.\n")
	b.WriteString("Record likes and dislikes only as personal taste under ## Personal context; never record instructions about how to respond (answer length, detail, tone, format, language, forms of address) — those are the user's standing instructions, a separate layer.\n")
	b.WriteString("Do NOT start facts with \"The user\" or similar filler subjects; drop filler words when meaning stays clear. ")
	b.WriteString("Write in a terse, telegraphic \"caveman\" style: keep only load-bearing words; drop articles (a/the), pronouns, and linking verbs (e.g. \"Lives Zurich; backend dev at Acme\"). ")
	b.WriteString("Drop chit-chat, one-off task details, and anything about other people except as it bears on the user. Replace outdated facts with their newest value instead of listing both. ")
	b.WriteString("NEVER store passwords, API keys, secrets, payment details, or other sensitive credentials. ")
	b.WriteString("Output ONLY the memory content, no preamble.")
	return b.String()
}

// GenerateMemory re-summarizes a memory. It is given a scope-specific header
// block (for example a project's name/description, or empty for user memory),
// the prior memory, a transcript of recent messages, and an optional
// excludedDirectives block (the user's explicit standing instructions that must
// NOT be duplicated into derived memory — empty for project memory), and returns
// a fresh, compact memory (re-summarize, not append) so the result stays bounded.
// The systemPrompt selects the memory's style (project vs. user). It takes plain
// strings to avoid a dependency on the chat package.
func (c *Client) GenerateMemory(ctx context.Context, header, priorMemory, transcript, excludedDirectives, systemPrompt string) (string, error) {
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
	if ex := strings.TrimSpace(excludedDirectives); ex != "" {
		b.WriteString("\nAlready saved as the user's explicit standing instructions — do NOT restate or duplicate any of these in the memory:\n\"\"\"\n")
		b.WriteString(ex)
		b.WriteString("\n\"\"\"\n")
	}
	b.WriteString("\nUpdated memory:")

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
