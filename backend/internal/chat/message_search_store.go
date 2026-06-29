package chat

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// defaultMessageSearchLimit caps how many full-text hits SearchMessages returns
// when the caller passes a non-positive limit. Kept small: the results feed a
// tool result that competes with the model's context budget.
const defaultMessageSearchLimit = 8

// MessageSearchHit is one full-text match: the matching user/assistant message
// plus the thread it belongs to. Hits are ordered by FTS5 bm25 relevance (most
// relevant first); ProjectID is nil for a project-less thread. Snippet is a
// match-centered excerpt (FTS5 snippet()), so a hit deep inside a long message
// still shows the relevant region rather than just the message's opening.
type MessageSearchHit struct {
	MessageID   string
	ThreadID    string
	ThreadTitle string
	ProjectID   *string
	Role        Role
	Snippet     string
	CreatedAt   time.Time
}

// SearchMessages runs a full-text search over the caller's own user/assistant
// message history (the message_fts index), most-relevant first. It is strictly
// user-scoped — only the caller's messages are searchable. When projectID is
// non-nil the search is restricted to threads in that project; when
// excludeThreadID is set, hits from that thread are dropped (e.g. the active
// conversation, which the model already has in context). Archived threads are
// excluded. Returns at most limit hits (defaultMessageSearchLimit when limit<=0).
//
// The query string is model/user-supplied free text, so it is converted to a
// safe FTS5 MATCH expression by buildFTSMatchQuery (each term quoted as a literal
// phrase) — raw text could otherwise raise "fts5: syntax error" on a stray quote
// or operator. An empty/blank query yields no hits rather than an error.
func (s *Store) SearchMessages(ctx context.Context, userID, query string, projectID *string, excludeThreadID string, limit int) ([]MessageSearchHit, error) {
	match := buildFTSMatchQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultMessageSearchLimit
	}

	var sb strings.Builder
	// snippet() centers the excerpt on the matched terms (content is column index
	// 3 in the FTS table: message_id, thread_id, user_id, content), wrapping each
	// match in «…» and bounding the window to ~32 tokens with … for elided text.
	sb.WriteString(`
SELECT m.id, m.thread_id, t.title, t.project_id, m.role,
       snippet(message_fts, 3, '«', '»', '…', 32) AS snippet,
       m.created_at
FROM message_fts
JOIN messages m ON m.id = message_fts.message_id
JOIN threads  t ON t.id = m.thread_id
WHERE message_fts MATCH ?
  AND message_fts.user_id = ?
  AND t.archived_at IS NULL`)
	args := []any{match, userID}
	if projectID != nil {
		sb.WriteString("\n  AND t.project_id = ?")
		args = append(args, *projectID)
	}
	if strings.TrimSpace(excludeThreadID) != "" {
		sb.WriteString("\n  AND m.thread_id <> ?")
		args = append(args, excludeThreadID)
	}
	sb.WriteString("\nORDER BY bm25(message_fts)\nLIMIT ?")
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	defer rows.Close()

	hits := make([]MessageSearchHit, 0, limit)
	for rows.Next() {
		var (
			hit       MessageSearchHit
			projID    sql.NullString
			role      string
			createdAt string
		)
		if err := rows.Scan(&hit.MessageID, &hit.ThreadID, &hit.ThreadTitle, &projID, &role, &hit.Snippet, &createdAt); err != nil {
			return nil, fmt.Errorf("scan message hit: %w", err)
		}
		if projID.Valid {
			id := projID.String
			hit.ProjectID = &id
		}
		hit.Role = Role(role)
		hit.CreatedAt, err = parseSQLiteTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse hit created_at: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message hits: %w", err)
	}
	return hits, nil
}

// buildFTSMatchQuery turns free-text into a safe FTS5 MATCH expression: each
// whitespace-separated term is wrapped in double quotes as a literal phrase, so
// FTS5 never interprets user/model text as query syntax (AND/OR/NOT, *, :,
// parentheses, a stray quote). Terms are space-joined, which FTS5 treats as an
// implicit AND — every term must appear in a matching message. Returns "" when
// the input has no usable terms.
func buildFTSMatchQuery(raw string) string {
	fields := strings.Fields(raw)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		// Escape embedded double-quotes per FTS5 rules ("" inside a "..." phrase).
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}
