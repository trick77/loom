package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// userFactColumnsF are the scanUserFact columns for a query aliasing
// user_facts as f.
const userFactColumnsF = "f.id, f.user_id, f.category, f.content, f.durable, f.origin, f.first_seen, f.last_seen"

// activeFactFilter selects a user's live facts. An extracted fact whose source
// threads are all deleted is hidden at once, before the next refresh retires
// it: deleting the threads a fact came from removes it, long-term or not.
const activeFactFilter = `f.user_id = ? AND f.retired_at IS NULL AND NOT (
    f.origin = 'extracted' AND
    NOT EXISTS (SELECT 1 FROM user_fact_sources s WHERE s.fact_id = f.id))`

func formatFactTime(t time.Time) string { return t.UTC().Format(time.DateTime) }

// ListActiveUserFacts returns the user's live facts, newest last_seen first.
func (s *Store) ListActiveUserFacts(ctx context.Context, userID string) ([]UserFact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+userFactColumnsF+`
FROM user_facts f
WHERE `+activeFactFilter+`
ORDER BY f.last_seen DESC, f.id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	facts := make([]UserFact, 0)
	for rows.Next() {
		fact, err := scanUserFact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user facts: %w", err)
	}
	return facts, nil
}

// ListForgottenUserFacts returns the contents of facts the user asked to forget
// that no refresh has seen yet. The next refresh gets them as "do not re-add":
// the forget request is itself a new message that names them.
func (s *Store) ListForgottenUserFacts(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT content FROM user_facts
WHERE user_id = ? AND retire_reason = 'user'
ORDER BY retired_at, id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list forgotten user facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	contents := make([]string, 0)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, fmt.Errorf("scan forgotten user fact: %w", err)
		}
		contents = append(contents, content)
	}
	return contents, rows.Err()
}

// ApplyUserFactBatch applies one refresh's ops in a single transaction.
// Invalid ops are dropped, not fatal: an unknown id, a bad category, content
// over MaxUserFactLength, or a retire without a valid SupersededBy (a live
// fact, or an add in the same batch). Adds run first so retires can name them.
func (s *Store) ApplyUserFactBatch(ctx context.Context, userID string, batch UserFactBatch) (UserFactApplyResult, error) {
	var res UserFactApplyResult
	if batch.Origin != FactOriginExtracted && batch.Origin != FactOriginConverted {
		return res, fmt.Errorf("apply user facts: origin %q", batch.Origin)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("apply user facts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	live, err := queryIDs(ctx, tx, `SELECT id FROM user_facts f WHERE `+activeFactFilter, userID)
	if err != nil {
		return res, fmt.Errorf("apply user facts: live ids: %w", err)
	}
	active := make(map[string]bool, len(live))
	for _, id := range live {
		active[id] = true
	}
	seen := formatFactTime(batch.SeenAt)
	refs := map[string]string{}

	for _, op := range batch.Ops {
		if op.Kind != FactOpAdd {
			continue
		}
		content, ok := validFactContent(op.Content)
		if !ok || !slices.Contains(FactCategories, op.Category) {
			res.Dropped++
			continue
		}
		id := sqlutil.NewID()
		last := opSeen(op, seen)
		first := last
		if batch.Origin == FactOriginConverted {
			first = seen
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_facts (id, user_id, category, content, durable, origin, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, userID, op.Category, content, op.Durable, batch.Origin, first, last,
		); err != nil {
			return res, fmt.Errorf("apply user facts: add: %w", err)
		}
		if err := addFactSources(ctx, tx, userID, id, opThreads(op, batch)); err != nil {
			return res, err
		}
		active[id] = true
		if op.Ref != "" {
			refs[op.Ref] = id
		}
		res.Added++
	}

	for _, op := range batch.Ops {
		switch op.Kind {
		case FactOpAdd:
		case FactOpUpdate, FactOpReinforce:
			if !active[op.ID] {
				res.Dropped++
				continue
			}
			if op.Kind == FactOpUpdate {
				content, ok := validFactContent(op.Content)
				if !ok {
					res.Dropped++
					continue
				}
				if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET content = ?, last_seen = MAX(last_seen, ?), updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, content, opSeen(op, seen), userID, op.ID); err != nil {
					return res, fmt.Errorf("apply user facts: update: %w", err)
				}
				res.Updated++
			} else {
				if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET last_seen = MAX(last_seen, ?), updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, opSeen(op, seen), userID, op.ID); err != nil {
					return res, fmt.Errorf("apply user facts: reinforce: %w", err)
				}
				res.Reinforced++
			}
			if err := addFactSources(ctx, tx, userID, op.ID, opThreads(op, batch)); err != nil {
				return res, err
			}
		case FactOpRetire:
			by := op.SupersededBy
			if ref, ok := refs[by]; ok {
				by = ref
			}
			if !active[op.ID] || by == "" || by == op.ID || !active[by] {
				res.Dropped++
				continue
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET retired_at = datetime('now'), retire_reason = 'superseded', superseded_by = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, by, userID, op.ID); err != nil {
				return res, fmt.Errorf("apply user facts: retire: %w", err)
			}
			active[op.ID] = false
			res.Retired++
		default:
			res.Dropped++
		}
	}

	if err := promoteAndRetireOrphans(ctx, tx, userID); err != nil {
		return res, err
	}
	if batch.PurgeForgottenBefore != nil {
		// Strictly before the second the refresh read them: retired_at has
		// second resolution, so a fact forgotten in that same second, maybe
		// after the read, waits for the next refresh instead of going unseen.
		if _, err := tx.ExecContext(ctx, `
DELETE FROM user_facts WHERE user_id = ? AND retire_reason = 'user' AND retired_at < ?`,
			userID, formatFactTime(batch.PurgeForgottenBefore.Truncate(time.Second)),
		); err != nil {
			return res, fmt.Errorf("apply user facts: purge forgotten: %w", err)
		}
	}
	if batch.Cursor != nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_fact_cursor (user_id, last_created_at, last_message_id, last_refresh_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(user_id) DO UPDATE SET
    last_created_at = excluded.last_created_at,
    last_message_id = excluded.last_message_id,
    last_refresh_at = excluded.last_refresh_at`,
			userID, batch.Cursor.LastCreatedAt, batch.Cursor.LastMessageID,
		); err != nil {
			return res, fmt.Errorf("apply user facts: cursor: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("apply user facts: commit: %w", err)
	}
	return res, nil
}

// promoteAndRetireOrphans marks facts still reinforced UserFactDurableSpan
// after first seen as long-term, and retires extracted facts whose source
// threads are all gone.
func promoteAndRetireOrphans(ctx context.Context, tx *sql.Tx, userID string) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET durable = 1, updated_at = datetime('now')
WHERE user_id = ? AND retired_at IS NULL AND durable = 0
  AND julianday(last_seen) - julianday(first_seen) >= ?`,
		userID, UserFactDurableSpan.Hours()/24,
	); err != nil {
		return fmt.Errorf("promote user facts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET retired_at = datetime('now'), retire_reason = 'thread_deleted', updated_at = datetime('now')
WHERE user_id = ? AND retired_at IS NULL AND origin = 'extracted'
  AND NOT EXISTS (SELECT 1 FROM user_fact_sources s WHERE s.fact_id = user_facts.id)`,
		userID,
	); err != nil {
		return fmt.Errorf("retire orphaned user facts: %w", err)
	}
	return nil
}

// AddUserFact stores a fact the user asked to remember in chat. threadID, when
// set, is recorded as its source; a tool-written fact never depends on it.
func (s *Store) AddUserFact(ctx context.Context, userID, threadID, category, content string, durable bool) (UserFact, error) {
	content, ok := validFactContent(content)
	if !ok || !slices.Contains(FactCategories, category) {
		return UserFact{}, ErrUserFactInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserFact{}, fmt.Errorf("add user fact: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id := sqlutil.NewID()
	now := formatFactTime(time.Now())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO user_facts (id, user_id, category, content, durable, origin, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, 'tool', ?, ?)`,
		id, userID, category, content, durable, now, now,
	); err != nil {
		return UserFact{}, fmt.Errorf("add user fact: %w", err)
	}
	if threadID != "" {
		if err := addFactSources(ctx, tx, userID, id, []string{threadID}); err != nil {
			return UserFact{}, err
		}
	}
	fact, err := getUserFactTx(ctx, tx, userID, id)
	if err != nil {
		return UserFact{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserFact{}, fmt.Errorf("add user fact: commit: %w", err)
	}
	return fact, nil
}

// UpdateUserFact rewrites a live fact on the user's request; it counts as a
// fresh mention (last_seen now) and keeps durable. The bool is false when no
// live fact has the id.
func (s *Store) UpdateUserFact(ctx context.Context, userID, threadID, id, content string) (UserFact, bool, error) {
	content, ok := validFactContent(content)
	if !ok {
		return UserFact{}, false, ErrUserFactInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserFact{}, false, fmt.Errorf("update user fact: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE user_facts SET content = ?, last_seen = MAX(last_seen, ?), updated_at = datetime('now')
WHERE id = ? AND `+activeFactFilterNoAlias,
		content, formatFactTime(time.Now()), id, userID,
	)
	if err != nil {
		return UserFact{}, false, fmt.Errorf("update user fact: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return UserFact{}, false, nil
	}
	if threadID != "" {
		if err := addFactSources(ctx, tx, userID, id, []string{threadID}); err != nil {
			return UserFact{}, false, err
		}
	}
	fact, err := getUserFactTx(ctx, tx, userID, id)
	if err != nil {
		return UserFact{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return UserFact{}, false, fmt.Errorf("update user fact: commit: %w", err)
	}
	return fact, true, nil
}

// ForgetUserFacts retires the given live facts on the user's request, durable
// ones included, and returns the facts it forgot. Unknown ids are skipped.
func (s *Store) ForgetUserFacts(ctx context.Context, userID string, ids []string) ([]UserFact, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("forget user facts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	forgotten := make([]UserFact, 0, len(ids))
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		fact, err := getUserFactTx(ctx, tx, userID, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `
UPDATE user_facts SET retired_at = datetime('now'), retire_reason = 'user', updated_at = datetime('now')
WHERE user_id = ? AND id = ? AND retired_at IS NULL`, userID, id)
		if err != nil {
			return nil, fmt.Errorf("forget user fact: %w", err)
		}
		if n, _ := result.RowsAffected(); n > 0 {
			forgotten = append(forgotten, fact)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("forget user facts: commit: %w", err)
	}
	return forgotten, nil
}

// MergeUserFacts folds two or more live facts into the first: it takes the
// merged content, the earliest first_seen, the latest last_seen, durable if any
// was, every source, and a protected origin if any had one. The rest are
// retired as superseded by it. Nothing is merged unless every id is live.
func (s *Store) MergeUserFacts(ctx context.Context, userID string, ids []string, content string) (bool, error) {
	content, ok := validFactContent(content)
	ids = uniqueInOrder(ids)
	if !ok || len(ids) < 2 {
		return false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("merge user facts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	facts := make([]UserFact, 0, len(ids))
	for _, id := range ids {
		var fact UserFact
		fact, err = scanUserFact(tx.QueryRowContext(ctx, `SELECT `+userFactColumnsF+` FROM user_facts f WHERE f.id = ? AND `+activeFactFilter, id, userID))
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("merge user facts: load: %w", err)
		}
		facts = append(facts, fact)
	}
	keep := facts[0]
	first, last, durable, origin := keep.FirstSeen, keep.LastSeen, keep.Durable, keep.Origin
	for _, f := range facts[1:] {
		if f.FirstSeen.Before(first) {
			first = f.FirstSeen
		}
		if f.LastSeen.After(last) {
			last = f.LastSeen
		}
		durable = durable || f.Durable
		if origin == FactOriginExtracted {
			origin = f.Origin
		}
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO user_fact_sources (fact_id, user_id, thread_id)
SELECT ?, user_id, thread_id FROM user_fact_sources WHERE fact_id = ?`, keep.ID, f.ID); err != nil {
			return false, fmt.Errorf("merge user facts: sources: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET retired_at = datetime('now'), retire_reason = 'superseded', superseded_by = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, keep.ID, userID, f.ID); err != nil {
			return false, fmt.Errorf("merge user facts: retire: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE user_facts SET content = ?, first_seen = ?, last_seen = ?, durable = ?, origin = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		content, formatFactTime(first), formatFactTime(last), durable, origin, userID, keep.ID); err != nil {
		return false, fmt.Errorf("merge user facts: keep: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("merge user facts: commit: %w", err)
	}
	return true, nil
}

// GetUserFactCursor returns the user's refresh cursor; false when no refresh
// or conversion ran yet.
func (s *Store) GetUserFactCursor(ctx context.Context, userID string) (UserFactCursor, bool, error) {
	var c UserFactCursor
	var refreshed, consolidated sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT last_created_at, last_message_id, last_refresh_at, last_consolidated_at
FROM user_fact_cursor WHERE user_id = ?`, userID,
	).Scan(&c.LastCreatedAt, &c.LastMessageID, &refreshed, &consolidated)
	if errors.Is(err, sql.ErrNoRows) {
		return UserFactCursor{}, false, nil
	}
	if err != nil {
		return UserFactCursor{}, false, fmt.Errorf("get user fact cursor: %w", err)
	}
	if c.LastRefreshAt, err = parseNullTime(refreshed); err != nil {
		return UserFactCursor{}, false, err
	}
	if c.LastConsolidatedAt, err = parseNullTime(consolidated); err != nil {
		return UserFactCursor{}, false, err
	}
	return c, true, nil
}

// MarkUserFactsConsolidated records that a consolidation pass ran.
func (s *Store) MarkUserFactsConsolidated(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO user_fact_cursor (user_id, last_consolidated_at) VALUES (?, datetime('now'))
ON CONFLICT(user_id) DO UPDATE SET last_consolidated_at = datetime('now')`, userID); err != nil {
		return fmt.Errorf("mark user facts consolidated: %w", err)
	}
	return nil
}

// ListUserMessagesAfter returns the user's messages after the cursor position
// (created_at, id), oldest first, up to limit, and only those created at or
// before settledBefore: a second is read only once no insert can still land
// in it, so a same-second message can never sort before the cursor and be
// skipped.
func (s *Store) ListUserMessagesAfter(ctx context.Context, userID, afterCreatedAt, afterID string, settledBefore time.Time, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = UserFactBatchLimit
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+transcriptColumnsM+`
FROM messages m
WHERE m.user_id = ? AND (m.created_at, m.id) > (?, ?) AND m.created_at <= ?
ORDER BY m.created_at, m.id
LIMIT ?`,
		userID, afterCreatedAt, afterID, formatFactTime(settledBefore), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list user messages after: %w", err)
	}
	defer func() { _ = rows.Close() }()
	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanTranscriptMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user messages: %w", err)
	}
	return messages, nil
}

// activeFactFilterNoAlias is activeFactFilter for an UPDATE on user_facts,
// which cannot alias the table.
const activeFactFilterNoAlias = `user_id = ? AND retired_at IS NULL AND NOT (
    origin = 'extracted' AND
    NOT EXISTS (SELECT 1 FROM user_fact_sources s WHERE s.fact_id = user_facts.id))`

// uniqueInOrder drops repeated ids, keeping the first occurrence: the first id
// of a merge is the fact that is kept.
func uniqueInOrder(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RenderFactLine renders one fact as "- [id] (category) content", the form
// both the system prompt and the fact tools use.
func RenderFactLine(f UserFact) string {
	return "- [" + f.ID + "] (" + f.Category + ") " + f.Content
}

func addFactSources(ctx context.Context, tx *sql.Tx, userID, factID string, threads []string) error {
	for _, threadID := range threads {
		// The thread may be gone by the time the refresh applies (deleted
		// mid-refresh); INSERT ... SELECT skips it instead of tripping the FK.
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO user_fact_sources (fact_id, user_id, thread_id)
SELECT ?, ?, id FROM threads WHERE user_id = ? AND id = ?`,
			factID, userID, userID, threadID,
		); err != nil {
			return fmt.Errorf("add user fact source: %w", err)
		}
	}
	return nil
}

// opSeen is the op's own date when it has one, else the batch's.
func opSeen(op UserFactOp, batchSeen string) string {
	if op.SeenAt.IsZero() {
		return batchSeen
	}
	return formatFactTime(op.SeenAt)
}

func opThreads(op UserFactOp, batch UserFactBatch) []string {
	if len(op.Threads) > 0 {
		return op.Threads
	}
	return batch.Threads
}

func getUserFactTx(ctx context.Context, tx *sql.Tx, userID, id string) (UserFact, error) {
	fact, err := scanUserFact(tx.QueryRowContext(ctx, `
SELECT `+userFactColumnsF+` FROM user_facts f
WHERE f.id = ? AND `+activeFactFilter, id, userID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return UserFact{}, fmt.Errorf("get user fact: %w", err)
	}
	return fact, err
}

func scanUserFact(row rowScanner) (UserFact, error) {
	var f UserFact
	var firstSeen, lastSeen string
	if err := row.Scan(&f.ID, &f.UserID, &f.Category, &f.Content, &f.Durable, &f.Origin, &firstSeen, &lastSeen); err != nil {
		return UserFact{}, err
	}
	var err error
	if f.FirstSeen, err = sqlutil.ParseTime(firstSeen); err != nil {
		return UserFact{}, fmt.Errorf("parse first_seen: %w", err)
	}
	if f.LastSeen, err = sqlutil.ParseTime(lastSeen); err != nil {
		return UserFact{}, fmt.Errorf("parse last_seen: %w", err)
	}
	return f, nil
}

func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func parseNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := sqlutil.ParseTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
