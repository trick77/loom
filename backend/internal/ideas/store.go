package ideas

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// ErrNotFound is an idea, thread or message the user does not have, or one
// the call may not see (a proposal outside Confirm, a deleted idea).
var ErrNotFound = errors.New("idea not found")

// ValidationError is input the store refuses; its message is safe to show.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// retention is how long a deleted idea stays restorable and an unconfirmed
// proposal is kept for its card.
const retention = "-30 days"

// DBTX is the subset of *sql.DB the store uses.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Store keeps ideas. Every query is scoped by user_id.
type Store struct {
	db DBTX
}

// NewStore returns a store on db.
func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

const ideaColumns = `id, user_id, state, title, reason, excerpt, thread_id, message_id, thread_title,
    had_thread = 1 AND thread_id IS NULL, due_on, remind_on, reminded_at, remind_dismissed_at,
    done_at, deleted_at, created_at, updated_at`

// visible is an idea the user sees: confirmed and not deleted.
const visible = `state IN ('open', 'done') AND deleted_at IS NULL`

type rowScanner interface{ Scan(...any) error }

func scanIdea(row rowScanner) (Idea, error) {
	var idea Idea
	var threadID, messageID, dueOn, remindOn, remindedAt, dismissedAt, doneAt, deletedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&idea.ID, &idea.UserID, &idea.State, &idea.Title, &idea.Reason, &idea.Excerpt,
		&threadID, &messageID, &idea.ThreadTitle, &idea.ThreadDeleted, &dueOn, &remindOn,
		&remindedAt, &dismissedAt, &doneAt, &deletedAt, &createdAt, &updatedAt); err != nil {
		return Idea{}, err
	}
	idea.ThreadID = nullString(threadID)
	idea.MessageID = nullString(messageID)
	idea.DueOn = nullString(dueOn)
	idea.RemindOn = nullString(remindOn)
	var err error
	for _, t := range []struct {
		raw sql.NullString
		dst **time.Time
	}{{remindedAt, &idea.RemindedAt}, {dismissedAt, &idea.RemindDismissedAt}, {doneAt, &idea.DoneAt}, {deletedAt, &idea.DeletedAt}} {
		if t.raw.Valid {
			v, err := sqlutil.ParseTime(t.raw.String)
			if err != nil {
				return Idea{}, fmt.Errorf("parse idea time: %w", err)
			}
			*t.dst = &v
		}
	}
	if idea.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return Idea{}, fmt.Errorf("parse created_at: %w", err)
	}
	if idea.UpdatedAt, err = sqlutil.ParseTime(updatedAt); err != nil {
		return Idea{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return idea, nil
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// fields is title, reason and excerpt as every write path stores them.
type fields struct {
	title, reason, excerpt string
}

// cleanFields caps and cleans an idea's text (CleanLine, CleanText). The
// reason is required; an empty title falls back to the reason.
func cleanFields(title, reason, excerpt string) (fields, error) {
	f := fields{
		title:   CleanLine(title, MaxTitleRunes),
		reason:  CleanLine(reason, MaxReasonRunes),
		excerpt: CleanText(excerpt, MaxExcerptRunes),
	}
	if f.reason == "" {
		return fields{}, invalid("reason is required")
	}
	if f.title == "" {
		f.title = CleanLine(f.reason, MaxTitleRunes)
	}
	return f, nil
}

// dates resolves due and follow-up dates. due and remind are the request's
// tri-state fields; oldDue and oldRemind the stored ones (nil for a new
// idea). A due date set without a follow-up gets the server's default.
func dates(due, remind OptionalDate, oldDue, oldRemind *string, today time.Time) (*string, *string, error) {
	newDue, newRemind := oldDue, oldRemind
	if due.Set {
		parsed, err := optionalDate("dueOn", due.Value)
		if err != nil {
			return nil, nil, err
		}
		newDue = parsed
		// Only a due date that changes gets the default: an edit form that
		// sends the stored one back keeps the follow-up the user picked.
		if !remind.Set && parsed != nil && !sameDate(parsed, oldDue) {
			newRemind = nil
			d, _ := ParseDate(*parsed)
			if r, ok := DefaultRemindOn(d, today); ok {
				s := FormatDate(r)
				newRemind = &s
			}
		}
	}
	if remind.Set {
		parsed, err := optionalDate("remindOn", remind.Value)
		if err != nil {
			return nil, nil, err
		}
		newRemind = parsed
	}
	if newDue != nil && newRemind != nil && *newRemind > *newDue {
		return nil, nil, invalid("the follow-up must not be after the due date")
	}
	return newDue, newRemind, nil
}

func optionalDate(name string, v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	d, err := ParseDate(*v)
	if err != nil {
		return nil, invalid("%s must be a date (YYYY-MM-DD)", name)
	}
	s := FormatDate(d)
	return &s, nil
}

// Create stores a new idea in state (StateOpen from the UI, StateProposed
// from the idea tools). The thread and message must be the user's, the
// message in that thread; the thread's title is kept as a snapshot. today
// is the user's civil date, for the default follow-up.
func (s *Store) Create(ctx context.Context, userID, state string, in Input, today time.Time) (Idea, error) {
	if state != StateOpen && state != StateProposed {
		return Idea{}, fmt.Errorf("create idea: bad state %q", state)
	}
	f, err := cleanFields(in.Title, in.Reason, in.Excerpt)
	if err != nil {
		return Idea{}, err
	}
	due, remind, err := dates(in.DueOn, in.RemindOn, nil, nil, today)
	if err != nil {
		return Idea{}, err
	}
	threadID, messageID := strings.TrimSpace(in.ThreadID), strings.TrimSpace(in.MessageID)
	if messageID != "" && threadID == "" {
		return Idea{}, invalid("messageId needs its threadId")
	}
	var threadTitle string
	if threadID != "" {
		err := s.db.QueryRowContext(ctx, `SELECT title FROM threads WHERE user_id = ? AND id = ?`, userID, threadID).Scan(&threadTitle)
		if errors.Is(err, sql.ErrNoRows) {
			return Idea{}, ErrNotFound
		}
		if err != nil {
			return Idea{}, fmt.Errorf("look up idea thread: %w", err)
		}
		threadTitle = CleanLine(threadTitle, 200)
	}
	if messageID != "" {
		var one int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE user_id = ? AND thread_id = ? AND id = ?`, userID, threadID, messageID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return Idea{}, ErrNotFound
		}
		if err != nil {
			return Idea{}, fmt.Errorf("look up idea message: %w", err)
		}
	}
	id := sqlutil.NewID()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ideas (id, user_id, state, title, reason, excerpt, thread_id, message_id, thread_title, had_thread, due_on, remind_on)
VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		id, userID, state, f.title, f.reason, f.excerpt, threadID, messageID, threadTitle, threadID != "", due, remind)
	if err != nil {
		return Idea{}, fmt.Errorf("insert idea: %w", err)
	}
	return s.GetAny(ctx, userID, id)
}

// get loads one of the user's ideas that matches where.
func (s *Store) get(ctx context.Context, userID, id, where string) (Idea, error) {
	idea, err := scanIdea(s.db.QueryRowContext(ctx, `SELECT `+ideaColumns+` FROM ideas WHERE user_id = ? AND id = ? AND `+where, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Idea{}, ErrNotFound
	}
	if err != nil {
		return Idea{}, fmt.Errorf("get idea: %w", err)
	}
	return idea, nil
}

// Get is one visible idea (open or done, not deleted).
func (s *Store) Get(ctx context.Context, userID, id string) (Idea, error) {
	return s.get(ctx, userID, id, visible)
}

// GetAny is one of the user's ideas in any state, deleted ones included: the
// idea card in a thread shows what became of its proposal.
func (s *Store) GetAny(ctx context.Context, userID, id string) (Idea, error) {
	return s.get(ctx, userID, id, "1 = 1")
}

// GetMany loads the user's ideas by id in any state, deleted ones included
// (the cards in a thread show what became of them). Unknown ids are absent.
func (s *Store) GetMany(ctx context.Context, userID string, ids []string) (map[string]Idea, error) {
	out := make(map[string]Idea, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+ideaColumns+` FROM ideas WHERE user_id = ? AND id IN (`+sqlutil.Placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("get ideas: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		out[idea.ID] = idea
	}
	return out, rows.Err()
}

// List is the user's open or done ideas. Open ones whose follow-up date has
// come (remind_on ≤ today) lead, then newest first; done ones by when they
// were done. It first purges what the 30 days of retention let go (Purge);
// the list never shows those rows anyway, so a purge that fails (the write
// lock is busy) is logged and the list still answers.
func (s *Store) List(ctx context.Context, userID, state string, today time.Time) ([]Idea, error) {
	if state != StateOpen && state != StateDone {
		return nil, invalid("status must be open or done")
	}
	if err := s.Purge(ctx, userID); err != nil {
		slog.Warn("purge ideas", "user_id", userID, "err", err)
	}
	order := `(remind_on IS NOT NULL AND remind_on <= ?) DESC, created_at DESC, rowid DESC`
	args := []any{userID, state, FormatDate(today)}
	if state == StateDone {
		order = `done_at DESC, rowid DESC`
		args = args[:2]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+ideaColumns+` FROM ideas WHERE user_id = ? AND state = ? AND deleted_at IS NULL ORDER BY `+order, args...)
	if err != nil {
		return nil, fmt.Errorf("list ideas: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]Idea, 0)
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		items = append(items, idea)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ideas: %w", err)
	}
	return items, nil
}

// Search is the user's open ideas, and done ones when asked, those whose
// title or reason contains query when one is given (case-insensitive):
// open first (due follow-ups, then newest), then done (latest first). One
// purge, one query: the idea tools' lookup.
func (s *Store) Search(ctx context.Context, userID string, includeDone bool, query string, today time.Time) ([]Idea, error) {
	if err := s.Purge(ctx, userID); err != nil {
		slog.Warn("purge ideas", "user_id", userID, "err", err)
	}
	states := `('open')`
	if includeDone {
		states = `('open', 'done')`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+ideaColumns+` FROM ideas
WHERE user_id = ? AND deleted_at IS NULL AND state IN `+states+`
ORDER BY state = 'done', (remind_on IS NOT NULL AND remind_on <= ?) DESC, COALESCE(done_at, created_at) DESC, rowid DESC`,
		userID, FormatDate(today))
	if err != nil {
		return nil, fmt.Errorf("search ideas: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// The match is case-folded here, not in SQL: SQLite's lower() and LIKE
	// fold ASCII only, and "Öl" must find "Ölheizung".
	q := strings.ToLower(strings.TrimSpace(query))
	items := make([]Idea, 0)
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		if q != "" && !strings.Contains(strings.ToLower(idea.Title), q) && !strings.Contains(strings.ToLower(idea.Reason), q) {
			continue
		}
		items = append(items, idea)
	}
	return items, rows.Err()
}

// FollowUp is an idea whose follow-up date may have come, with its owner's
// zone: whether it has is the owner's date, not the server's.
type FollowUp struct {
	Idea     Idea
	Timezone string
}

// FollowUpWindowDays is how long after its follow-up date an idea without a
// due date still counts as one: the start page lists it that long, and the
// notifier drops one older than that unsent.
const FollowUpWindowDays = 14

// FollowUpCursor is where the last page of FollowUpsDue ended; the zero
// value starts at the beginning.
type FollowUpCursor struct{ RemindOn, ID string }

// FollowUpsDue is a page of the open ideas not yet followed up, and not
// hidden, whose follow-up date is on or before latest (the latest date
// anywhere on earth), in (remind_on, id) order after the cursor. The caller
// decides per user's zone and claims each (ClaimFollowUp); it pages on, so
// candidates not yet due in their zone never hide ones that are.
func (s *Store) FollowUpsDue(ctx context.Context, latest time.Time, after FollowUpCursor, limit int) ([]FollowUp, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+ideaColumns+`, (SELECT timezone FROM users u WHERE u.id = ideas.user_id)
FROM ideas
WHERE reminded_at IS NULL AND remind_dismissed_at IS NULL
  AND remind_on IS NOT NULL AND remind_on <= ?
  AND state = 'open' AND deleted_at IS NULL
  AND (remind_on > ? OR (remind_on = ? AND id > ?))
ORDER BY remind_on, id
LIMIT ?`, FormatDate(latest), after.RemindOn, after.RemindOn, after.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list follow-ups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []FollowUp
	for rows.Next() {
		var f FollowUp
		var tz sql.NullString
		idea, err := scanIdea(scanAppend{rows, &tz})
		if err != nil {
			return nil, fmt.Errorf("scan follow-up: %w", err)
		}
		f.Idea, f.Timezone = idea, tz.String
		out = append(out, f)
	}
	return out, rows.Err()
}

// scanAppend scans a row into an idea's columns plus extra ones after them.
type scanAppend struct {
	rows  *sql.Rows
	extra any
}

func (s scanAppend) Scan(dest ...any) error { return s.rows.Scan(append(dest, s.extra)...) }

// ClaimFollowUp marks an idea's follow-up as sent, once: false when it was
// claimed already, moved, done or deleted meanwhile. The notification goes
// out only after a claim (the two stores share no transaction, and one
// notification too many is worse than one missed).
func (s *Store) ClaimFollowUp(ctx context.Context, userID, id, remindOn string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE ideas SET reminded_at = datetime('now')
WHERE user_id = ? AND id = ? AND reminded_at IS NULL AND remind_on = ?
  AND remind_dismissed_at IS NULL AND state = 'open' AND deleted_at IS NULL`, userID, id, remindOn)
	if err != nil {
		return false, fmt.Errorf("claim follow-up: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// FollowUpsForToday is what the start page lists under "Follow-up": open
// ideas whose follow-up date has come and that the user has not hidden, up
// to limit, soonest due first. One with a due date shows until that date;
// one without, for two weeks after its follow-up date.
func (s *Store) FollowUpsForToday(ctx context.Context, userID string, today time.Time, limit int) ([]Idea, error) {
	day := FormatDate(today)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+ideaColumns+` FROM ideas
WHERE user_id = ? AND state = 'open' AND deleted_at IS NULL
  AND remind_on IS NOT NULL AND remind_on <= ? AND remind_dismissed_at IS NULL
  AND ((due_on IS NOT NULL AND due_on >= ?) OR (due_on IS NULL AND remind_on >= ?))
ORDER BY COALESCE(due_on, remind_on), rowid
LIMIT ?`, userID, day, day, FormatDate(today.AddDate(0, 0, -FollowUpWindowDays)), limit)
	if err != nil {
		return nil, fmt.Errorf("list follow-ups for today: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]Idea, 0)
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		items = append(items, idea)
	}
	return items, rows.Err()
}

// Purge deletes the user's ideas deleted more than 30 days ago and the
// proposals nobody confirmed within 30 days.
func (s *Store) Purge(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM ideas
WHERE user_id = ? AND (
    (deleted_at IS NOT NULL AND deleted_at < datetime('now', ?)) OR
    (state = 'proposed' AND created_at < datetime('now', ?)))`,
		userID, retention, retention)
	return err
}

// Count is how many visible ideas are open and done.
func (s *Store) Count(ctx context.Context, userID string) (open, done int, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(state = 'open'), 0), COALESCE(SUM(state = 'done'), 0)
FROM ideas WHERE user_id = ? AND `+visible, userID).Scan(&open, &done)
	if err != nil {
		return 0, 0, fmt.Errorf("count ideas: %w", err)
	}
	return open, done, nil
}

// Update changes a visible idea. It writes only the columns the patch
// touches, so two tabs patching different fields do not undo each other. A
// follow-up date that changes starts over: not yet notified, not hidden from
// the start page (unless the same patch hides it).
func (s *Store) Update(ctx context.Context, userID, id string, p Patch, today time.Time) (Idea, error) {
	idea, err := s.Get(ctx, userID, id)
	if err != nil {
		return Idea{}, err
	}
	var sets []string
	var args []any
	set := func(clause string, values ...any) {
		sets = append(sets, clause)
		args = append(args, values...)
	}
	if p.Title != nil || p.Reason != nil || p.Excerpt != nil {
		title, reason, excerpt := idea.Title, idea.Reason, idea.Excerpt
		if p.Title != nil {
			title = *p.Title
		}
		if p.Reason != nil {
			reason = *p.Reason
		}
		if p.Excerpt != nil {
			excerpt = *p.Excerpt
		}
		f, err := cleanFields(title, reason, excerpt)
		if err != nil {
			return Idea{}, err
		}
		if p.Title != nil || f.title != idea.Title {
			set("title = ?", f.title)
		}
		if p.Reason != nil {
			set("reason = ?", f.reason)
		}
		if p.Excerpt != nil {
			set("excerpt = ?", f.excerpt)
		}
	}
	if p.DueOn.Set || p.RemindOn.Set {
		due, remind, err := dates(p.DueOn, p.RemindOn, idea.DueOn, idea.RemindOn, today)
		if err != nil {
			return Idea{}, err
		}
		set("due_on = ?, remind_on = ?", due, remind)
		if !sameDate(remind, idea.RemindOn) {
			set("reminded_at = NULL, remind_dismissed_at = NULL")
		}
	}
	if p.RemindDismissed != nil {
		if *p.RemindDismissed {
			set("remind_dismissed_at = datetime('now')")
		} else {
			set("remind_dismissed_at = NULL")
		}
	}
	if p.Done != nil {
		if *p.Done {
			set("state = 'done', done_at = COALESCE(done_at, datetime('now'))")
		} else {
			set("state = 'open', done_at = NULL")
		}
	}
	if len(sets) == 0 {
		return idea, nil
	}
	set("updated_at = datetime('now')")
	_, err = s.db.ExecContext(ctx, `UPDATE ideas SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id = ? AND `+visible,
		append(args, userID, id)...)
	if err != nil {
		return Idea{}, fmt.Errorf("update idea: %w", err)
	}
	return s.Get(ctx, userID, id)
}

func sameDate(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Delete removes an idea: a visible one softly (restorable for 30 days), a
// proposal for good (its card's "Dismiss").
func (s *Store) Delete(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE ideas SET deleted_at = datetime('now'), updated_at = datetime('now')
WHERE user_id = ? AND id = ? AND `+visible, userID, id)
	if err != nil {
		return fmt.Errorf("delete idea: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	res, err = s.db.ExecContext(ctx, `DELETE FROM ideas WHERE user_id = ? AND id = ? AND state = 'proposed'`, userID, id)
	if err != nil {
		return fmt.Errorf("discard proposal: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore brings back an idea deleted within the last 30 days.
func (s *Store) Restore(ctx context.Context, userID, id string) (Idea, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE ideas SET deleted_at = NULL, updated_at = datetime('now')
WHERE user_id = ? AND id = ? AND state IN ('open', 'done')
  AND deleted_at IS NOT NULL AND deleted_at >= datetime('now', ?)`, userID, id, retention)
	if err != nil {
		return Idea{}, fmt.Errorf("restore idea: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Idea{}, ErrNotFound
	}
	return s.Get(ctx, userID, id)
}

// Confirm turns a proposal into an open idea: the user's click on its card,
// the only way out of StateProposed.
func (s *Store) Confirm(ctx context.Context, userID, id string) (Idea, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE ideas SET state = 'open', updated_at = datetime('now')
WHERE user_id = ? AND id = ? AND state = 'proposed' AND deleted_at IS NULL
  AND created_at >= datetime('now', ?)`, userID, id, retention)
	if err != nil {
		return Idea{}, fmt.Errorf("confirm idea: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Idea{}, ErrNotFound
	}
	return s.Get(ctx, userID, id)
}
