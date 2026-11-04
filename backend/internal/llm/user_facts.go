package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/trick77/toolkit/llmwire"
)

// User memory is one row per fact; these helpers ask the model for changes to
// that list, never for a rewrite. llm does not import chat, so the facts and
// ops cross as the plain types below.

// FactInput is one existing fact as the model sees it.
type FactInput struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Durable  bool   `json:"long_term,omitempty"`
	LastSeen string `json:"last_seen"`
}

// FactOp is one change the model asks for. Threads are transcript thread
// labels (T1, T2, ...); the caller maps them to thread ids.
type FactOp struct {
	Op           string   `json:"op"`
	Ref          string   `json:"ref,omitempty"`
	ID           string   `json:"id,omitempty"`
	Category     string   `json:"category,omitempty"`
	Content      string   `json:"content,omitempty"`
	Durable      bool     `json:"long_term,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	Threads      []string `json:"threads,omitempty"`
}

// FactMerge folds near-duplicate facts into one.
type FactMerge struct {
	IDs     []string `json:"ids"`
	Content string   `json:"content"`
}

// ErrFactReplyMalformed reports a reply that is not the asked-for JSON. The
// caller applies nothing and retries on the next sweep.
var ErrFactReplyMalformed = errors.New("user fact reply malformed")

const factRules = "Each fact is ONE terse, telegraphic fragment under 300 characters: keep only load-bearing words, no \"The user\" prefix (e.g. \"Backend dev at Acme, Go + SQLite\", \"Lives Zurich\"). " +
	"category is one of work, personal, interest. " +
	"Never record how the assistant should respond (answer length, tone, format, language, forms of address): those are the user's standing instructions, a separate layer. " +
	"Never store passwords, API keys, secrets, payment details or other credentials. " +
	"Skip chit-chat, one-off task details, and other people except as they bear on the user."

// UserFactsSystemPrompt drives the background refresh: fold new messages into
// the fact list as ops.
const UserFactsSystemPrompt = "You maintain a durable list of facts about the user so the assistant stays personalized across all of their chats. " +
	"You get the current facts (with ids and when each was last seen), and a transcript of NEW messages grouped by thread label (T1, T2, ...) with dates. " +
	"Output ONLY a JSON object {\"ops\": [...]} with these op kinds:\n" +
	"- {\"op\":\"add\",\"ref\":\"n1\",\"category\":\"work\",\"content\":\"...\",\"long_term\":false,\"threads\":[\"T1\"]}: a new fact about the user — who they are, what they do and build, where they live, their projects, goals, tastes and hobbies. Set long_term true only for settled identity facts expected to hold for years (home city, profession, family, languages). ref is a short local name other ops in this reply may point to.\n" +
	"- {\"op\":\"update\",\"id\":\"...\",\"content\":\"...\",\"threads\":[\"T2\"]}: an existing fact changed or became more precise, same subject.\n" +
	"- {\"op\":\"reinforce\",\"id\":\"...\",\"threads\":[\"T1\"]}: the new messages mention or rely on an existing fact again without changing it. Emit this for EVERY existing fact the new messages touch, even in passing: it is how the memory knows a fact is still current.\n" +
	"- {\"op\":\"retire\",\"id\":\"...\",\"superseded_by\":\"<id or ref>\"}: only when another fact replaces it (moved city: retire the old city, superseded_by the new one). Never retire a fact for being old, unmentioned or unimportant: old facts stay.\n" +
	"threads is REQUIRED on add, update and reinforce: only the labels of the threads the op actually comes from (deleting those threads removes the fact). Do not add a fact that duplicates an existing one: reinforce or update it instead. " +
	factRules + " " +
	"If nothing about the user changed, output {\"ops\": []}."

// UserFactsConsolidateSystemPrompt merges near-duplicate facts.
const UserFactsConsolidateSystemPrompt = "You tidy a list of facts about a user. Find facts that state the same thing or overlap so much they should be one. " +
	"Output ONLY a JSON object {\"merges\": [{\"ids\":[\"id1\",\"id2\"],\"content\":\"...\"}]}, the merged content keeping every detail of the merged facts. " +
	"Only merge true duplicates or overlaps, never unrelated facts, and never drop information. " +
	"If nothing should be merged, output {\"merges\": []}. " +
	factRules

// ExtractUserFacts asks for the ops that fold a transcript of new messages into
// the facts. excluded are the user's standing instructions (never restated as
// facts); forgotten are facts the user asked to forget since the last refresh
// (never re-added: the forget request itself is in the transcript).
func (c *Client) ExtractUserFacts(ctx context.Context, facts []FactInput, transcript, excluded string, forgotten []string, responseLanguage string) ([]FactOp, error) {
	var b strings.Builder
	b.WriteString("Current facts (JSON):\n")
	writeJSON(&b, facts)
	b.WriteString("\nNew messages:\n\"\"\"\n")
	b.WriteString(strings.TrimSpace(transcript))
	b.WriteString("\n\"\"\"\n")
	if ex := strings.TrimSpace(excluded); ex != "" {
		b.WriteString("\nAlready saved as the user's standing instructions — never restate these as facts:\n\"\"\"\n")
		b.WriteString(ex)
		b.WriteString("\n\"\"\"\n")
	}
	if len(forgotten) > 0 {
		b.WriteString("\nThe user asked to forget these. Do NOT add, update or restate them, even if the messages mention them:\n\"\"\"\n- ")
		b.WriteString(strings.Join(forgotten, "\n- "))
		b.WriteString("\n\"\"\"\n")
	}
	var reply struct {
		Ops []FactOp `json:"ops"`
	}
	if err := c.factCompletion(ctx, UserFactsSystemPrompt, b.String(), responseLanguage, &reply); err != nil {
		return nil, err
	}
	return reply.Ops, nil
}

// ConsolidateUserFacts asks which facts to merge.
func (c *Client) ConsolidateUserFacts(ctx context.Context, facts []FactInput, responseLanguage string) ([]FactMerge, error) {
	var b strings.Builder
	b.WriteString("Facts (JSON):\n")
	writeJSON(&b, facts)
	var reply struct {
		Merges []FactMerge `json:"merges"`
	}
	if err := c.factCompletion(ctx, UserFactsConsolidateSystemPrompt, b.String(), responseLanguage, &reply); err != nil {
		return nil, err
	}
	return reply.Merges, nil
}

// factCompletion runs one fact helper call and decodes its JSON object into
// out. A truncated reply is ErrMemoryTruncated and a reply without the JSON is
// ErrFactReplyMalformed: either way nothing may be applied.
func (c *Client) factCompletion(ctx context.Context, systemPrompt, user, responseLanguage string, out any) error {
	reply, err := c.prose(ctx, []Message{
		{Role: "system", Content: appendLanguageDirective(systemPrompt, responseLanguage) + " JSON keys stay in English."},
		{Role: "user", Content: user},
	}, memoryMaxCompletionTokens)
	if err != nil {
		return err
	}
	if reply.Empty {
		return fmt.Errorf("user fact completion returned no choices")
	}
	if reply.FinishReason == "length" {
		return fmt.Errorf("%w: completion_tokens=%d reasoning_tokens=%d answer_cap=%d",
			ErrMemoryTruncated, reply.Usage.CompletionTokens, reply.Usage.CompletionTokenDetails.ReasoningTokens, memoryMaxCompletionTokens)
	}
	raw, found := llmwire.JSONObject(reply.Content)
	if !found {
		return ErrFactReplyMalformed
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("%w: %w", ErrFactReplyMalformed, err)
	}
	return nil
}

func writeJSON(b *strings.Builder, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("[]")
	}
	b.Write(data)
	b.WriteString("\n")
}
