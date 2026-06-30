package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrDirectivesBudgetExceeded is returned by AddUserDirective/ReplaceUserDirective
// when a write would push a single directive over MaxUserDirectiveLength or the
// user's combined directives over MaxUserDirectivesTotalLength. Unlike derived
// memory, directives are NOT silently truncated: the write is refused so the
// chat tool can tell the model (and the user) to remove or shorten one first.
var ErrDirectivesBudgetExceeded = errors.New("directives budget exceeded")

// ListUserDirectives returns the user's standing instructions in stable insertion
// order.
func (s *Store) ListUserDirectives(ctx context.Context, userID string) ([]UserDirective, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, content, position, created_at, updated_at
FROM user_directives
WHERE user_id = ?
ORDER BY position, created_at, rowid`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user directives: %w", err)
	}
	defer rows.Close()

	directives := make([]UserDirective, 0)
	for rows.Next() {
		directive, err := scanUserDirective(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user directive: %w", err)
		}
		directives = append(directives, directive)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user directives: %w", err)
	}
	return directives, nil
}

// AddUserDirective inserts a new directive. It refuses (without writing) when the
// content is blank, exceeds MaxUserDirectiveLength, or would push the user's
// combined directive length past MaxUserDirectivesTotalLength, returning
// ErrDirectivesBudgetExceeded in the latter two cases.
func (s *Store) AddUserDirective(ctx context.Context, userID, content string) (UserDirective, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return UserDirective{}, errors.New("directive content is required")
	}
	if len([]rune(content)) > MaxUserDirectiveLength {
		return UserDirective{}, ErrDirectivesBudgetExceeded
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserDirective{}, fmt.Errorf("add user directive: begin: %w", err)
	}
	defer tx.Rollback()

	existing, err := directivesTotalLength(ctx, tx, userID, "")
	if err != nil {
		return UserDirective{}, err
	}
	if existing+len([]rune(content)) > MaxUserDirectivesTotalLength {
		return UserDirective{}, ErrDirectivesBudgetExceeded
	}

	var position int
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(position), -1) + 1 FROM user_directives WHERE user_id = ?`,
		userID,
	).Scan(&position); err != nil {
		return UserDirective{}, fmt.Errorf("add user directive: next position: %w", err)
	}

	id := newID()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO user_directives (id, user_id, content, position, created_at, updated_at)
VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		id, userID, content, position,
	); err != nil {
		return UserDirective{}, fmt.Errorf("add user directive: insert: %w", err)
	}

	directive, err := getUserDirectiveTx(ctx, tx, userID, id)
	if err != nil {
		return UserDirective{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserDirective{}, fmt.Errorf("add user directive: commit: %w", err)
	}
	return directive, nil
}

// RemoveUserDirective deletes one directive by id (user-scoped). The bool is false
// when no row matched, so the tool can report "no such instruction".
func (s *Store) RemoveUserDirective(ctx context.Context, userID, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM user_directives WHERE user_id = ? AND id = ?`,
		userID, id,
	)
	if err != nil {
		return false, fmt.Errorf("remove user directive: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("remove user directive: rows affected: %w", err)
	}
	return affected > 0, nil
}

// ReplaceUserDirective overwrites one directive's content by id, with the same
// per-item and total-budget enforcement as AddUserDirective (computed as the
// existing total minus the old content plus the new content). The bool is false
// when the id is not found.
func (s *Store) ReplaceUserDirective(ctx context.Context, userID, id, content string) (UserDirective, bool, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return UserDirective{}, false, errors.New("directive content is required")
	}
	if len([]rune(content)) > MaxUserDirectiveLength {
		return UserDirective{}, false, ErrDirectivesBudgetExceeded
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserDirective{}, false, fmt.Errorf("replace user directive: begin: %w", err)
	}
	defer tx.Rollback()

	// Not-found takes precedence over a budget rejection: a bogus/stale id should
	// report "no such instruction", not a misleading "budget full".
	var exists int
	switch err := tx.QueryRowContext(ctx, `
SELECT 1 FROM user_directives WHERE user_id = ? AND id = ?`,
		userID, id,
	).Scan(&exists); {
	case err == sql.ErrNoRows:
		return UserDirective{}, false, nil
	case err != nil:
		return UserDirective{}, false, fmt.Errorf("replace user directive: lookup: %w", err)
	}

	existing, err := directivesTotalLength(ctx, tx, userID, id)
	if err != nil {
		return UserDirective{}, false, err
	}
	if existing+len([]rune(content)) > MaxUserDirectivesTotalLength {
		return UserDirective{}, false, ErrDirectivesBudgetExceeded
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE user_directives SET content = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		content, userID, id,
	); err != nil {
		return UserDirective{}, false, fmt.Errorf("replace user directive: update: %w", err)
	}

	directive, err := getUserDirectiveTx(ctx, tx, userID, id)
	if err != nil {
		return UserDirective{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return UserDirective{}, false, fmt.Errorf("replace user directive: commit: %w", err)
	}
	return directive, true, nil
}

// directivesTotalLength sums the rune length of a user's directives, optionally
// excluding one id (used by Replace so the row being overwritten isn't
// double-counted). It runs inside the caller's transaction.
func directivesTotalLength(ctx context.Context, tx *sql.Tx, userID, excludeID string) (int, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT content FROM user_directives WHERE user_id = ? AND id != ?`,
		userID, excludeID,
	)
	if err != nil {
		return 0, fmt.Errorf("directives total length: %w", err)
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return 0, fmt.Errorf("directives total length: scan: %w", err)
		}
		total += len([]rune(content))
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("directives total length: iterate: %w", err)
	}
	return total, nil
}

func getUserDirectiveTx(ctx context.Context, tx *sql.Tx, userID, id string) (UserDirective, error) {
	directive, err := scanUserDirective(tx.QueryRowContext(ctx, `
SELECT id, user_id, content, position, created_at, updated_at
FROM user_directives
WHERE user_id = ? AND id = ?`,
		userID, id,
	))
	if err != nil {
		return UserDirective{}, fmt.Errorf("get user directive: %w", err)
	}
	return directive, nil
}

func scanUserDirective(row rowScanner) (UserDirective, error) {
	var directive UserDirective
	var createdAt, updatedAt string
	if err := row.Scan(&directive.ID, &directive.UserID, &directive.Content, &directive.Position, &createdAt, &updatedAt); err != nil {
		return UserDirective{}, err
	}
	created, err := parseSQLiteTime(createdAt)
	if err != nil {
		return UserDirective{}, fmt.Errorf("parse created_at: %w", err)
	}
	updated, err := parseSQLiteTime(updatedAt)
	if err != nil {
		return UserDirective{}, fmt.Errorf("parse updated_at: %w", err)
	}
	directive.CreatedAt = created
	directive.UpdatedAt = updated
	return directive, nil
}
