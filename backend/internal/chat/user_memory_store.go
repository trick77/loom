package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/trick77/loom/internal/sqlutil"
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
	if errors.Is(err, sql.ErrNoRows) {
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

func scanUserMemory(row rowScanner) (UserMemory, error) {
	var memory UserMemory
	var updatedAt string
	if err := row.Scan(&memory.Content, &memory.SourceMessageCount, &updatedAt); err != nil {
		return UserMemory{}, err
	}
	parsed, err := sqlutil.ParseTime(updatedAt)
	if err != nil {
		return UserMemory{}, fmt.Errorf("parse updated_at: %w", err)
	}
	memory.UpdatedAt = &parsed
	return memory, nil
}
