package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateThread(ctx context.Context, userID string, in CreateThreadInput) (Thread, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = DefaultThreadTitle
	}
	if len(title) > MaxThreadTitleLength {
		return Thread{}, errors.New("thread title is too long")
	}

	var projectID any
	if in.ProjectID != nil {
		if ok, err := s.projectExists(ctx, userID, *in.ProjectID); err != nil {
			return Thread{}, err
		} else if !ok {
			return Thread{}, errors.New("project not found")
		}
		projectID = *in.ProjectID
	}

	threadID := newID()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, project_id, title)
VALUES (?, ?, ?, ?)`,
		threadID, userID, projectID, title,
	)
	if err != nil {
		return Thread{}, fmt.Errorf("insert thread: %w", err)
	}

	thread, ok, err := s.GetThread(ctx, userID, threadID)
	if err != nil {
		return Thread{}, err
	}
	if !ok {
		return Thread{}, errors.New("inserted thread not found")
	}
	return thread, nil
}

func (s *Store) GetThread(ctx context.Context, userID, threadID string) (Thread, bool, error) {
	return s.getThread(ctx, userID, threadID)
}

func (s *Store) ListThreads(ctx context.Context, userID string, opts ListThreadsOptions) ([]Thread, error) {
	if opts.ProjectID != nil && opts.ProjectlessOnly {
		return nil, errors.New("project filter cannot be combined with projectless filter")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	filters := []string{"user_id = ?"}
	args := []any{userID}
	if opts.Archived {
		filters = append(filters, "archived_at IS NOT NULL")
	} else {
		filters = append(filters, "archived_at IS NULL")
	}
	if opts.ProjectID != nil {
		filters = append(filters, "project_id = ?")
		args = append(args, *opts.ProjectID)
	}
	if opts.ProjectlessOnly {
		filters = append(filters, "project_id IS NULL")
	}
	if opts.StarredOnly {
		filters = append(filters, "starred = 1")
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
SELECT id, user_id, project_id, title, starred, archived_at, created_at, updated_at, last_message_at
FROM threads
WHERE %s
ORDER BY COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC
LIMIT ?`, strings.Join(filters, " AND "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	threads := make([]Thread, 0)
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate threads: %w", err)
	}
	return threads, nil
}

func (s *Store) UpdateThread(ctx context.Context, userID, threadID string, in UpdateThreadInput) (Thread, bool, error) {
	thread, ok, err := s.GetThread(ctx, userID, threadID)
	if err != nil || !ok {
		return Thread{}, ok, err
	}

	title := thread.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
		if title == "" {
			return Thread{}, false, errors.New("thread title is required")
		}
		if len(title) > MaxThreadTitleLength {
			return Thread{}, false, errors.New("thread title is too long")
		}
	}

	_, err = s.db.ExecContext(ctx, `
UPDATE threads
SET title = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		title, userID, threadID,
	)
	if err != nil {
		return Thread{}, false, fmt.Errorf("update thread: %w", err)
	}
	return s.GetThread(ctx, userID, threadID)
}

func (s *Store) SetThreadStarred(ctx context.Context, userID, threadID string, starred bool) (Thread, bool, error) {
	starredInt := 0
	if starred {
		starredInt = 1
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE threads
SET starred = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		starredInt, userID, threadID,
	)
	if err != nil {
		return Thread{}, false, fmt.Errorf("star thread: %w", err)
	}
	ok, err := changed(result)
	if err != nil || !ok {
		return Thread{}, ok, err
	}
	return s.GetThread(ctx, userID, threadID)
}

func (s *Store) SetThreadArchived(ctx context.Context, userID, threadID string, archived bool) (bool, error) {
	setArchivedAt := "archived_at = NULL"
	if archived {
		setArchivedAt = "archived_at = datetime('now')"
	}
	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
UPDATE threads
SET %s, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, setArchivedAt),
		userID, threadID,
	)
	if err != nil {
		return false, fmt.Errorf("archive thread: %w", err)
	}
	return changed(result)
}

func (s *Store) DeleteThread(ctx context.Context, userID, threadID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM threads
WHERE user_id = ? AND id = ?`,
		userID, threadID,
	)
	if err != nil {
		return false, fmt.Errorf("delete thread: %w", err)
	}
	return changed(result)
}

func (s *Store) getThread(ctx context.Context, userID, threadID string) (Thread, bool, error) {
	thread, err := scanThread(s.db.QueryRowContext(ctx, `
SELECT id, user_id, project_id, title, starred, archived_at, created_at, updated_at, last_message_at
FROM threads
WHERE user_id = ? AND id = ?`,
		userID, threadID,
	))
	if err == nil {
		return thread, true, nil
	}
	if err == sql.ErrNoRows {
		return Thread{}, false, nil
	}
	return Thread{}, false, fmt.Errorf("get thread: %w", err)
}

func (s *Store) threadExists(ctx context.Context, userID, threadID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `
SELECT 1
FROM threads
WHERE user_id = ? AND id = ?`,
		userID, threadID,
	).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check thread: %w", err)
}
