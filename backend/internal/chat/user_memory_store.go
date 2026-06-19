package chat

import (
	"context"
	"database/sql"
	"fmt"
)

// GetUserMemory returns the stored memory for a user. The bool is false when no
// memory has been generated yet.
func (s *Store) GetUserMemory(ctx context.Context, userID string) (UserMemory, bool, error) {
	memory, err := scanUserMemory(s.db.QueryRowContext(ctx, `
SELECT content, source_message_count, updated_at
FROM user_memory
WHERE user_id = ?`,
		userID,
	))
	if err == nil {
		return memory, true, nil
	}
	if err == sql.ErrNoRows {
		return UserMemory{}, false, nil
	}
	return UserMemory{}, false, fmt.Errorf("get user memory: %w", err)
}

// UpsertUserMemory stores (creating or replacing) the user's memory. The content
// is re-summarized on every refresh, so this overwrites rather than appends.
func (s *Store) UpsertUserMemory(ctx context.Context, userID, content string, sourceMessageCount int) (UserMemory, error) {
	// Truncate on rune boundaries so the hard cap can never split a multi-byte
	// character and produce invalid UTF-8.
	if runes := []rune(content); len(runes) > MaxUserMemoryLength {
		content = string(runes[:MaxUserMemoryLength])
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_memory (user_id, content, source_message_count, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(user_id) DO UPDATE SET
    content = excluded.content,
    source_message_count = excluded.source_message_count,
    updated_at = datetime('now')`,
		userID, content, sourceMessageCount,
	)
	if err != nil {
		return UserMemory{}, fmt.Errorf("upsert user memory: %w", err)
	}
	memory, _, err := s.GetUserMemory(ctx, userID)
	return memory, err
}

// CountUserMessages returns the total number of messages across every thread the
// user owns (project-bound or not). It gates the background memory refresh.
func (s *Store) CountUserMessages(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM messages
WHERE user_id = ?`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user messages: %w", err)
	}
	return count, nil
}

// ListUserMessages returns the user's most recent messages across all threads
// (chronological order), capped at limit. Used for the full memory rebuild; the
// cap keeps the rebuild bounded so it never loads the entire history.
func (s *Store) ListUserMessages(ctx context.Context, userID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.thread_id, m.role, m.content, m.reasoning_content, m.tool_calls, m.citations, m.artifacts, m.attachments, m.activity_trace, m.content_blocks, m.prompt_tokens, m.completion_tokens, m.total_tokens, m.cached_tokens, m.reasoning_tokens, m.duration_ms, m.model, m.reasoning_effort, m.created_at
FROM messages m
WHERE m.user_id = ?
ORDER BY m.created_at DESC, m.rowid DESC
LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list user messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user messages: %w", err)
	}
	// Fetched newest-first to apply the cap; reverse to chronological so the
	// summary reads the conversation in order.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func scanUserMemory(row rowScanner) (UserMemory, error) {
	var memory UserMemory
	var updatedAt string
	if err := row.Scan(&memory.Content, &memory.SourceMessageCount, &updatedAt); err != nil {
		return UserMemory{}, err
	}
	parsed, err := parseSQLiteTime(updatedAt)
	if err != nil {
		return UserMemory{}, fmt.Errorf("parse updated_at: %w", err)
	}
	memory.UpdatedAt = &parsed
	return memory, nil
}
