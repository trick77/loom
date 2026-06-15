package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) AddMessage(ctx context.Context, userID, threadID string, role Role, content string) (Message, error) {
	return s.AddMessageWithUsage(ctx, userID, threadID, role, content, MessageTokenUsage{})
}

func (s *Store) AddMessageWithUsage(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage) (Message, error) {
	return s.AddMessageWithArtifacts(ctx, userID, threadID, role, content, usage, nil)
}

func (s *Store) AddMessageWithArtifacts(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage) (Message, error) {
	return s.AddMessageWithActivityTrace(ctx, userID, threadID, role, content, usage, artifacts, nil)
}

func (s *Store) AddMessageWithActivityTrace(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage) (Message, error) {
	return s.AddMessageWithCitations(ctx, userID, threadID, role, content, usage, artifacts, activityTrace, nil)
}

// AddMessageWithCitations is the full message insert, additionally persisting RAG
// citations (the documents whose chunks informed the answer). citations may be
// nil for turns without retrieval.
func (s *Store) AddMessageWithCitations(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage, artifacts json.RawMessage, activityTrace json.RawMessage, citations json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{
		userID:        userID,
		threadID:      threadID,
		role:          role,
		content:       content,
		usage:         usage,
		artifacts:     artifacts,
		activityTrace: activityTrace,
		citations:     citations,
	})
}

// AddMessageWithAttachments persists a message together with the attachments the
// user sent with it (uploaded images and attached documents), so the sent
// previews survive a reload. attachments may be nil for a message without any.
func (s *Store) AddMessageWithAttachments(ctx context.Context, userID, threadID string, role Role, content string, attachments json.RawMessage) (Message, error) {
	return s.insertMessage(ctx, messageInsert{
		userID:      userID,
		threadID:    threadID,
		role:        role,
		content:     content,
		attachments: attachments,
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
}

func (s *Store) insertMessage(ctx context.Context, in messageInsert) (Message, error) {
	userID, threadID, role, content := in.userID, in.threadID, in.role, in.content
	usage := in.usage
	artifacts, activityTrace, citations, attachments := in.artifacts, in.activityTrace, in.citations, in.attachments
	if role != RoleUser && role != RoleAssistant && role != RoleTool {
		return Message{}, fmt.Errorf("invalid message role %q", role)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return Message{}, errors.New("message content is required")
	}
	if len(content) > MaxMessageContentLength {
		return Message{}, errors.New("message content is too long")
	}
	if ok, err := s.threadExists(ctx, userID, threadID); err != nil {
		return Message{}, err
	} else if !ok {
		return Message{}, errors.New("thread not found")
	}
	if len(artifacts) == 0 {
		artifacts = json.RawMessage("[]")
	}
	if !json.Valid(artifacts) {
		return Message{}, errors.New("message artifacts must be valid JSON")
	}
	if len(activityTrace) == 0 {
		activityTrace = json.RawMessage("[]")
	}
	if !json.Valid(activityTrace) {
		return Message{}, errors.New("message activity trace must be valid JSON")
	}
	if len(citations) == 0 {
		citations = json.RawMessage("[]")
	}
	if !json.Valid(citations) {
		return Message{}, errors.New("message citations must be valid JSON")
	}
	if len(attachments) == 0 {
		attachments = json.RawMessage("[]")
	}
	if !json.Valid(attachments) {
		return Message{}, errors.New("message attachments must be valid JSON")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("begin message transaction: %w", err)
	}
	defer tx.Rollback()

	messageID := newID()
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
    activity_trace,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    duration_ms,
    model,
    reasoning_effort
)
VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID,
		threadID,
		userID,
		role,
		content,
		usage.ReasoningContent,
		string(citations),
		string(artifacts),
		string(attachments),
		string(activityTrace),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CachedTokens,
		usage.ReasoningTokens,
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

func (s *Store) ListMessages(ctx context.Context, userID, threadID string) ([]Message, bool, error) {
	if ok, err := s.threadExists(ctx, userID, threadID); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, activity_trace, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, duration_ms, model, reasoning_effort, created_at
FROM messages
WHERE user_id = ? AND thread_id = ?
ORDER BY created_at ASC, id ASC`,
		userID, threadID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

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
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, artifacts, attachments, activity_trace, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, duration_ms, model, reasoning_effort, created_at
FROM messages
WHERE user_id = ? AND id = ?`,
		userID, messageID,
	))
	if err == nil {
		return message, true, nil
	}
	if err == sql.ErrNoRows {
		return Message{}, false, nil
	}
	return Message{}, false, fmt.Errorf("get message: %w", err)
}
