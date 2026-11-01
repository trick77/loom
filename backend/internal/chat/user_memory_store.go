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
