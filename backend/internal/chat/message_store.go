package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/trick77/loom/internal/sqlutil"
)

// AddMessage inserts a message with only identity, role and content.
func (s *Store) AddMessage(ctx context.Context, userID, threadID string, role Role, content string) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content})
}

// AddMessageWithUsage inserts a message and records the turn's token counts,
// cost and other metrics.
func (s *Store) AddMessageWithUsage(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage})
}

// AddMessageWithArtifacts inserts a message together with its generated
// artifacts (a JSON array).
func (s *Store) AddMessageWithArtifacts(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage, artifacts: artifacts})
}

// AddMessageWithActivityTrace inserts a message together with its artifacts
// and the activity trace recording its tool calls.
func (s *Store) AddMessageWithActivityTrace(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{userID: userID, threadID: threadID, role: role, content: content, usage: usage, artifacts: artifacts, activityTrace: activityTrace})
}

// AddMessageWithCitations is the full assistant insert: artifacts, activity
// trace, citations (the documents and web sources behind the answer) and the
// ordered content blocks. Any of the JSON columns may be nil.
func (s *Store) AddMessageWithCitations(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage, citations json.RawMessage, contentBlocks json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{
		userID:        userID,
		threadID:      threadID,
		role:          role,
		content:       content,
		usage:         usage,
		artifacts:     artifacts,
		activityTrace: activityTrace,
		citations:     citations,
		contentBlocks: contentBlocks,
	})
}

// AddMessageWithAttachments persists a message together with the attachments the
// user sent with it (uploaded images and attached documents), so the sent
// previews survive a reload. attachments may be nil for a message without any.
func (s *Store) AddMessageWithAttachments(ctx context.Context, userID, threadID string, role Role, content string, attachments json.RawMessage, pastedTexts json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{
		userID:      userID,
		threadID:    threadID,
		role:        role,
		content:     content,
		attachments: attachments,
		pastedTexts: pastedTexts,
	})
}

// messageInsert carries the optional fields of a message insert; every field
// beyond the required identity/content has a sensible empty default so the
// thin public AddMessage* wrappers can set only what they need.
type messageInsert struct {
	userID        string
	threadID      string
	role          Role
	content       string
	usage         MessageTokenUsage
	artifacts     json.RawMessage
	activityTrace json.RawMessage
	citations     json.RawMessage
	attachments   json.RawMessage
	pastedTexts   json.RawMessage
	contentBlocks json.RawMessage
}

func (s *Store) insertMessage(ctx context.Context, in messageInsert) (Message, error) {
	userID, threadID, role, content := in.userID, in.threadID, in.role, in.content
	usage := in.usage
	artifacts, activityTrace, citations, attachments := in.artifacts, in.activityTrace, in.citations, in.attachments
	contentBlocks := in.contentBlocks
	pastedTexts := in.pastedTexts
	if role != RoleUser && role != RoleAssistant && role != RoleTool {
		return Message{}, fmt.Errorf("invalid message role %q", role)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return Message{}, validation("message content is required")
	}
	if len(content) > maxContentLengthForRole(role) {
		return Message{}, validation("message content is too long")
	}
	if ok, err := s.threadExists(ctx, userID, threadID); err != nil {
		return Message{}, err
	} else if !ok {
		return Message{}, ErrThreadNotFound
	}
	jsonColumns := []struct {
		name string
		raw  *json.RawMessage
	}{
		{"artifacts", &artifacts},
		{"activity trace", &activityTrace},
		{"citations", &citations},
		{"attachments", &attachments},
		{"content blocks", &contentBlocks},
		{"pasted texts", &pastedTexts},
	}
	for _, column := range jsonColumns {
		normalized, err := normalizeJSONArray(column.name, *column.raw)
		if err != nil {
			return Message{}, err
		}
		*column.raw = normalized
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("begin message transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	messageID := sqlutil.NewID()
	_, err = tx.ExecContext(ctx, `
INSERT INTO messages (
    id,
    thread_id,
    user_id,
    role,
    content,
    reasoning_content,
    tool_calls,
    citations,
    artifacts,
    attachments,
    pasted_texts,
    activity_trace,
    content_blocks,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    context_tokens,
    cost_nano_usd,
    duration_ms,
    model,
    reasoning_effort
)
VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID,
		threadID,
		userID,
		role,
		content,
		usage.ReasoningContent,
		string(citations),
		string(artifacts),
		string(attachments),
		string(pastedTexts),
		string(activityTrace),
		string(contentBlocks),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CachedTokens,
		usage.ReasoningTokens,
		usage.ContextTokens,
		usage.CostNanoUSD,
		usage.DurationMs,
		usage.Model,
		usage.ReasoningEffort,
	)
	if err != nil {
		return Message{}, fmt.Errorf("insert message: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
UPDATE threads
SET last_message_at = (SELECT created_at FROM messages WHERE user_id = ? AND id = ?),
    updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		userID, messageID, userID, threadID,
	)
	if err != nil {
		return Message{}, fmt.Errorf("update thread message timestamp: %w", err)
	}

	// A new message is user activity in the owning project: bump its
	// last_activity_at so the project card and "Recent activity" sort reflect it.
	// No-op for project-less threads (the subquery yields no matching row).
	_, err = tx.ExecContext(ctx, `
UPDATE projects
SET last_activity_at = datetime('now')
WHERE user_id = ? AND id = (
    SELECT project_id FROM threads
    WHERE user_id = ? AND id = ? AND project_id IS NOT NULL)`,
		userID, userID, threadID,
	)
	if err != nil {
		return Message{}, fmt.Errorf("update project activity timestamp: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("commit message transaction: %w", err)
	}

	message, ok, err := s.getMessage(ctx, userID, messageID)
	if err != nil {
		return Message{}, err
	}
	if !ok {
		return Message{}, errors.New("inserted message not found")
	}
	return message, nil
}

// ListRecentMessages returns at most limit of a thread's most recent messages,
// in chronological (ascending) order. Unlike ListMessages it never loads the
// whole transcript, so callers that only need the tail (e.g. the cross-thread
// summary digest, which keeps each thread's final turns) don't pull hundreds of
// messages — including large tool-result blobs — just to discard them.
func (s *Store) ListRecentMessages(ctx context.Context, userID, threadID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, pasted_texts, activity_trace, content_blocks, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, context_tokens, cost_nano_usd, duration_ms, model, reasoning_effort, created_at
FROM messages
WHERE user_id = ? AND thread_id = ?
ORDER BY rowid DESC
LIMIT ?`,
		userID, threadID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recent message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent messages: %w", err)
	}
	// Fetched newest-first to apply the cap; reverse to chronological so the tail
	// reads in conversation order.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// ListMessages returns all messages in a thread in insertion order. rowid, not
// created_at: the timestamp has one-second resolution and ids are random, so a
// question and its quick reply would otherwise come back in either order. The
// bool indicates whether the thread exists; false is returned if the thread is
// not found.
func (s *Store) ListMessages(ctx context.Context, userID, threadID string) ([]Message, bool, error) {
	if ok, err := s.threadExists(ctx, userID, threadID); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, pasted_texts, activity_trace, content_blocks, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, context_tokens, cost_nano_usd, duration_ms, model, reasoning_effort, created_at
FROM messages
WHERE user_id = ? AND thread_id = ?
ORDER BY rowid ASC`,
		userID, threadID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, false, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, true, nil
}

func (s *Store) getMessage(ctx context.Context, userID, messageID string) (Message, bool, error) {
	message, err := scanMessage(s.db.QueryRowContext(ctx, `
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, pasted_texts, activity_trace, content_blocks, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, context_tokens, cost_nano_usd, duration_ms, model, reasoning_effort, created_at
FROM messages
WHERE user_id = ? AND id = ?`,
		userID, messageID,
	))
	if err == nil {
		return message, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, false, nil
	}
	return Message{}, false, fmt.Errorf("get message: %w", err)
}

// maxContentLengthForRole picks the content cap by author: user text is bounded
// by what one send may carry, model output by the generous assistant cap.
func maxContentLengthForRole(role Role) int {
	if role == RoleUser {
		return MaxMessageContentLength
	}
	return MaxAssistantMessageContentLength
}

// normalizeJSONArray defaults an absent JSON column to an empty array and
// rejects one that is not valid JSON, naming the column in the error.
func normalizeJSONArray(name string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("[]"), nil
	}
	if !json.Valid(raw) {
		return nil, validation("message " + name + " must be valid JSON")
	}
	return raw, nil
}
