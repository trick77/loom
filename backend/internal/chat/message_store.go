package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) AddMessage(ctx context.Context, userID, threadID string, role Role, content string) (Message, error) {
	return s.AddMessageWithUsage(ctx, userID, threadID, role, content, MessageTokenUsage{})
}

func (s *Store) AddMessageWithUsage(ctx context.Context, userID, threadID string, role Role, content string, usage MessageTokenUsage) (Message, error) {
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
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID,
		threadID,
		userID,
		role,
		content,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CachedTokens,
		usage.ReasoningTokens,
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
SELECT id, thread_id, role, content, tool_calls, citations, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, created_at
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
SELECT id, thread_id, role, content, tool_calls, citations, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, created_at
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
