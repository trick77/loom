package chat

import (
	"context"
	"database/sql"
	"fmt"
)

// GetProjectMemory returns the stored memory for a project. The bool is false
// when no memory has been generated yet.
func (s *Store) GetProjectMemory(ctx context.Context, userID, projectID string) (ProjectMemory, bool, error) {
	memory, err := scanProjectMemory(s.db.QueryRowContext(ctx, `
SELECT project_id, content, source_message_count, updated_at
FROM project_memory
WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	))
	if err == nil {
		return memory, true, nil
	}
	if err == sql.ErrNoRows {
		return ProjectMemory{ProjectID: projectID}, false, nil
	}
	return ProjectMemory{}, false, fmt.Errorf("get project memory: %w", err)
}

// UpsertProjectMemory stores (creating or replacing) the project's memory. The
// content is re-summarized on every refresh, so this overwrites rather than
// appends.
func (s *Store) UpsertProjectMemory(ctx context.Context, userID, projectID, content string, sourceMessageCount int) (ProjectMemory, error) {
	if len(content) > MaxProjectMemoryLength {
		content = content[:MaxProjectMemoryLength]
	}
	if ok, err := s.projectExists(ctx, userID, projectID); err != nil {
		return ProjectMemory{}, err
	} else if !ok {
		return ProjectMemory{}, fmt.Errorf("project not found")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_memory (user_id, project_id, content, source_message_count, updated_at)
VALUES (?, ?, ?, ?, datetime('now'))
ON CONFLICT(user_id, project_id) DO UPDATE SET
    content = excluded.content,
    source_message_count = excluded.source_message_count,
    updated_at = datetime('now')`,
		userID, projectID, content, sourceMessageCount,
	)
	if err != nil {
		return ProjectMemory{}, fmt.Errorf("upsert project memory: %w", err)
	}
	memory, _, err := s.GetProjectMemory(ctx, userID, projectID)
	return memory, err
}

// CountProjectMessages returns the total number of messages across every thread
// in the project. It gates the background memory refresh.
func (s *Store) CountProjectMessages(ctx context.Context, userID, projectID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM messages m
JOIN threads t ON t.user_id = m.user_id AND t.id = m.thread_id
WHERE m.user_id = ? AND t.project_id = ?`,
		userID, projectID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count project messages: %w", err)
	}
	return count, nil
}

// ListProjectMessages returns the most recent messages across all threads in the
// project (chronological order), capped at limit. Used for the full memory
// rebuild; the cap keeps the rebuild bounded so it never loads the entire
// project history.
func (s *Store) ListProjectMessages(ctx context.Context, userID, projectID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.thread_id, m.role, m.content, m.reasoning_content, m.tool_calls, m.citations, m.artifacts, m.activity_trace, m.prompt_tokens, m.completion_tokens, m.total_tokens, m.cached_tokens, m.reasoning_tokens, m.duration_ms, m.model, m.reasoning_effort, m.created_at
FROM messages m
JOIN threads t ON t.user_id = m.user_id AND t.id = m.thread_id
WHERE m.user_id = ? AND t.project_id = ?
ORDER BY m.created_at DESC, m.rowid DESC
LIMIT ?`,
		userID, projectID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list project messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project messages: %w", err)
	}
	// Fetched newest-first to apply the cap; reverse to chronological so the
	// summary reads the conversation in order.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func scanProjectMemory(row rowScanner) (ProjectMemory, error) {
	var memory ProjectMemory
	var updatedAt string
	if err := row.Scan(&memory.ProjectID, &memory.Content, &memory.SourceMessageCount, &updatedAt); err != nil {
		return ProjectMemory{}, err
	}
	parsed, err := parseSQLiteTime(updatedAt)
	if err != nil {
		return ProjectMemory{}, fmt.Errorf("parse updated_at: %w", err)
	}
	memory.UpdatedAt = &parsed
	return memory, nil
}
