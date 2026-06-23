package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) CreateThread(ctx context.Context, userID string, in CreateThreadInput) (Thread, error) {
	title := NormalizeThreadTitle(in.Title)
	if title == "" {
		title = DefaultThreadTitle
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

// threadFilters builds the shared WHERE clauses (and their args) for thread
// listing, excluding cursor keyset and limit so both ListThreads and
// ListThreadIDs stay in sync on which rows match.
func threadFilters(userID string, opts ListThreadsOptions) ([]string, []any, error) {
	if opts.ProjectID != nil && opts.ProjectlessOnly {
		return nil, nil, errors.New("project filter cannot be combined with projectless filter")
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
	if search := strings.TrimSpace(opts.Search); search != "" {
		filters = append(filters, `title LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(search)+"%")
	}
	// Derive chat visibility from the owning project's archived state instead of
	// writing to threads. In the resting lists (no project scope, no search,
	// active threads) hide chats whose project is archived; search and project
	// detail intentionally bypass this so archived-project chats stay findable
	// and visible when the project is opened.
	if opts.ProjectID == nil && strings.TrimSpace(opts.Search) == "" && !opts.Archived {
		filters = append(filters,
			"(project_id IS NULL OR NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = threads.project_id AND p.user_id = ? AND p.archived_at IS NOT NULL))")
		args = append(args, userID)
	}
	return filters, args, nil
}

func (s *Store) ListThreads(ctx context.Context, userID string, opts ListThreadsOptions) ([]Thread, error) {
	filters, args, err := threadFilters(userID, opts)
	if err != nil {
		return nil, err
	}
	limit := EffectiveThreadLimit(opts.Limit)

	if opts.Cursor != "" {
		cursor, err := decodeThreadCursor(opts.Cursor)
		if err != nil {
			return nil, err
		}
		// Keyset bound for ORDER BY (activity, updated_at, id) DESC. SQLite
		// row-value comparison preserves the exact tie-break ordering.
		filters = append(filters, "(COALESCE(last_message_at, updated_at), updated_at, id) < (?, ?, ?)")
		args = append(args, cursor.Activity, cursor.Updated, cursor.ID)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
SELECT id, user_id, project_id, title, category, starred, archived_at, created_at, updated_at, last_message_at
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

// ListThreadIDs returns the ids of every thread matching the same filters as
// ListThreads (search/archived/project), without limit or cursor. Used by the
// "select all matches" flow so the client can act on threads it hasn't loaded.
func (s *Store) ListThreadIDs(ctx context.Context, userID string, opts ListThreadsOptions) ([]string, error) {
	filters, args, err := threadFilters(userID, opts)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
SELECT id
FROM threads
WHERE %s
ORDER BY COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC`, strings.Join(filters, " AND "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list thread ids: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan thread id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread ids: %w", err)
	}
	return ids, nil
}

func (s *Store) UpdateThread(ctx context.Context, userID, threadID string, in UpdateThreadInput) (Thread, bool, error) {
	thread, ok, err := s.GetThread(ctx, userID, threadID)
	if err != nil || !ok {
		return Thread{}, ok, err
	}

	title := thread.Title
	if in.Title != nil {
		title = NormalizeThreadTitle(*in.Title)
		if title == "" {
			return Thread{}, false, errors.New("thread title is required")
		}
	}

	var projectID any
	if thread.ProjectID != nil {
		projectID = *thread.ProjectID
	}
	if in.ProjectID.Set {
		projectID = nil
		if in.ProjectID.Value != nil {
			if ok, err := s.projectExists(ctx, userID, *in.ProjectID.Value); err != nil {
				return Thread{}, false, err
			} else if !ok {
				return Thread{}, false, errors.New("project not found")
			}
			projectID = *in.ProjectID.Value
		}
	}

	category := thread.Category
	if in.Category != nil {
		category = *in.Category
	}

	_, err = s.db.ExecContext(ctx, `
UPDATE threads
SET title = ?, category = ?, project_id = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		title, category, projectID, userID, threadID,
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

// escapeLike escapes the LIKE wildcards so a user search term matches literally.
// Used together with `ESCAPE '\'` in the query.
func escapeLike(term string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(term)
}

func (s *Store) getThread(ctx context.Context, userID, threadID string) (Thread, bool, error) {
	thread, err := scanThread(s.db.QueryRowContext(ctx, `
SELECT id, user_id, project_id, title, category, starred, archived_at, created_at, updated_at, last_message_at
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
