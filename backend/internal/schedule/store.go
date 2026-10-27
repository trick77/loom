package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// MaxSchedulesPerUser bounds how many schedules one account keeps.
const MaxSchedulesPerUser = 10

// Field limits, enforced by the service before anything is stored.
const (
	MaxNameLength = 120
	// MaxPromptLength keeps a schedule's task to what a short run covers: a
	// 3 KB research brief once ran past the time limit.
	MaxPromptLength   = 2000
	MaxIntervalLength = 200
)

// Run statuses.
const (
	StatusRunning    = "running"
	StatusNew        = "new"
	StatusNothingNew = "nothing_new"
	StatusTokenLimit = "token_limit"
	StatusTimeout    = "timeout"
	StatusFailed     = "failed"
)

// DisabledTimeout is the reason loom gives a schedule it disabled after a run
// went past the time limit.
const DisabledTimeout = "timeout"

// Notification kinds.
const (
	KindScheduleRun      = "schedule_run"
	KindScheduleDisabled = "schedule_disabled"
)

// ErrNotFound is a schedule that does not exist for the user.
var ErrNotFound = errors.New("schedule not found")

// ErrLimitReached is a create past MaxSchedulesPerUser.
var ErrLimitReached = fmt.Errorf("at most %d schedules per account", MaxSchedulesPerUser)

// DBTX is the subset of *sql.DB the store uses.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store persists schedules, their runs and memory, and notifications. Every
// query is scoped by user id, except the worker's claim across all users.
type Store struct {
	db DBTX
}

// NewStore returns a store backed by db.
func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

// Schedule is one prompt that runs on its own.
type Schedule struct {
	ID             string     `json:"id"`
	UserID         string     `json:"-"`
	Name           string     `json:"name"`
	Prompt         string     `json:"prompt"`
	IntervalText   string     `json:"interval"`
	CronTemplate   string     `json:"-"`
	Cron           string     `json:"cron"`
	Timezone       string     `json:"timezone"`
	Enabled        bool       `json:"enabled"`
	NextRunAt      *time.Time `json:"nextRunAt"`
	Running        bool       `json:"running"`
	DisabledReason string     `json:"disabledReason"`
	DisabledAt     *time.Time `json:"disabledAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Run is one execution of a schedule.
type Run struct {
	ID         string `json:"id"`
	ScheduleID string `json:"scheduleId"`
	// ThreadID is the thread the run started; nil once the user deleted it.
	ThreadID    *string    `json:"threadId"`
	MessageID   *string    `json:"messageId"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	Status      string     `json:"status"`
	NewCount    int        `json:"newCount"`
	Rounds      int        `json:"rounds"`
	Tokens      int64      `json:"tokens"`
	CostNanoUSD int64      `json:"-"`
	DurationMS  int64      `json:"durationMs"`
	Error       string     `json:"error"`
	// NewLabels are the items this run reported.
	NewLabels []string `json:"newLabels"`
}

// Finding is one item a run reported.
type Finding struct {
	Label     string
	SourceURL string
}

// Source is what the runs learned about one page.
type Source struct {
	URLKey        string     `json:"url"`
	Prefer        string     `json:"prefer"`
	FetchFailedAt *time.Time `json:"fetchFailedAt"`
	BrowserOKAt   *time.Time `json:"browserOkAt"`
	Runs          int        `json:"runs"`
	Finds         int        `json:"finds"`
	LastUsedAt    time.Time  `json:"lastUsedAt"`
}

// Query is a search a run issued and the finds it led to.
type Query struct {
	Query      string    `json:"query"`
	Runs       int       `json:"runs"`
	Finds      int       `json:"finds"`
	EmptyRuns  int       `json:"emptyRuns"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

// Notification is one item in the user's inbox.
type Notification struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	ScheduleID *string    `json:"scheduleId"`
	ThreadID   *string    `json:"threadId"`
	MessageID  *string    `json:"messageId"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReadAt     *time.Time `json:"readAt"`
}

// ts formats a time the way SQLite's datetime('now') does, so stored times
// compare as strings.
func ts(t time.Time) string { return t.UTC().Format(time.DateTime) }

func nullTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func parseNull(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := sqlutil.ParseTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

const scheduleColumns = "id, user_id, name, prompt, interval_text, cron_template, cron, timezone, enabled, next_run_at, running_at, disabled_reason, disabled_at, created_at, updated_at"

type rowScanner interface{ Scan(...any) error }

func scanSchedule(row rowScanner) (Schedule, error) {
	var s Schedule
	var nextRunAt, runningAt, disabledReason, disabledAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.UserID, &s.Name, &s.Prompt, &s.IntervalText, &s.CronTemplate, &s.Cron, &s.Timezone, &s.Enabled,
		&nextRunAt, &runningAt, &disabledReason, &disabledAt, &createdAt, &updatedAt); err != nil {
		return Schedule{}, err
	}
	s.Running = runningAt.Valid
	s.DisabledReason = disabledReason.String
	var err error
	if s.NextRunAt, err = parseNull(nextRunAt); err != nil {
		return Schedule{}, err
	}
	if s.DisabledAt, err = parseNull(disabledAt); err != nil {
		return Schedule{}, err
	}
	if s.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return Schedule{}, err
	}
	if s.UpdatedAt, err = sqlutil.ParseTime(updatedAt); err != nil {
		return Schedule{}, err
	}
	return s, nil
}

// CreateSchedule inserts s for its user, refusing past MaxSchedulesPerUser.
// The count and the insert are one statement, so two concurrent creates cannot
// both slip under the limit.
func (st *Store) CreateSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	id := sqlutil.NewID()
	res, err := st.db.ExecContext(ctx, `
INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone, enabled, next_run_at)
SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE (SELECT COUNT(*) FROM schedules WHERE user_id = ?) < ?`,
		id, s.UserID, s.Name, s.Prompt, s.IntervalText, s.CronTemplate, s.Cron, s.Timezone, s.Enabled, nullTS(s.NextRunAt),
		s.UserID, MaxSchedulesPerUser)
	if err != nil {
		return Schedule{}, fmt.Errorf("insert schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Schedule{}, ErrLimitReached
	}
	return st.GetSchedule(ctx, s.UserID, id)
}

// GetSchedule returns one of the user's schedules, or ErrNotFound.
func (st *Store) GetSchedule(ctx context.Context, userID, id string) (Schedule, error) {
	s, err := scanSchedule(st.db.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	return s, nil
}

// ListSchedules returns the user's schedules, oldest first.
func (st *Store) ListSchedules(ctx context.Context, userID string) ([]Schedule, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveSchedule writes the editable fields of s back: name, prompt, interval,
// cron, timezone, enabled, next run and the disabled reason.
func (st *Store) SaveSchedule(ctx context.Context, s Schedule) (Schedule, error) {
	var reason any
	if s.DisabledReason != "" {
		reason = s.DisabledReason
	}
	res, err := st.db.ExecContext(ctx, `
UPDATE schedules
SET name = ?, prompt = ?, interval_text = ?, cron_template = ?, cron = ?, timezone = ?, enabled = ?,
    next_run_at = ?, disabled_reason = ?, disabled_at = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`,
		s.Name, s.Prompt, s.IntervalText, s.CronTemplate, s.Cron, s.Timezone, s.Enabled,
		nullTS(s.NextRunAt), reason, nullTS(s.DisabledAt), s.UserID, s.ID)
	if err != nil {
		return Schedule{}, fmt.Errorf("save schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Schedule{}, ErrNotFound
	}
	return st.GetSchedule(ctx, s.UserID, s.ID)
}

// DeleteSchedule removes the schedule with its runs and memory. Run threads
// still here stay, as ordinary threads.
func (st *Store) DeleteSchedule(ctx context.Context, userID, id string) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM schedules WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DueSchedules lists enabled, idle schedules whose next run is at or before
// now, across all users, earliest first.
func (st *Store) DueSchedules(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT `+scheduleColumns+` FROM schedules
WHERE enabled = 1 AND running_at IS NULL AND next_run_at IS NOT NULL AND next_run_at <= ?
ORDER BY next_run_at LIMIT ?`, ts(now), limit)
	if err != nil {
		return nil, fmt.Errorf("due schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Claim marks the schedule running for a run outside its slots ("Run now"),
// keeping its next run. It reports false when another run already holds it.
func (st *Store) Claim(ctx context.Context, userID, id string, now time.Time) (bool, error) {
	return st.claim(ctx, `UPDATE schedules SET running_at = ?
WHERE user_id = ? AND id = ? AND running_at IS NULL`, ts(now), userID, id)
}

// ClaimDue marks a due schedule running and moves its next run to next. nil
// leaves it without one: a schedule whose next slot cannot be computed must
// not stay due and run on every tick.
func (st *Store) ClaimDue(ctx context.Context, userID, id string, now time.Time, next *time.Time) (bool, error) {
	return st.claim(ctx, `UPDATE schedules SET running_at = ?, next_run_at = ?
WHERE user_id = ? AND id = ? AND running_at IS NULL`, ts(now), nullTS(next), userID, id)
}

func (st *Store) claim(ctx context.Context, query string, args ...any) (bool, error) {
	res, err := st.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("claim schedule: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// Release ends the schedule's run. A slot that passed while it ran (a "Run
// now" just before it) moves on to next, so the schedule does not run again
// right after.
func (st *Store) Release(ctx context.Context, userID, id string, now time.Time, next *time.Time) error {
	_, err := st.db.ExecContext(ctx, `
UPDATE schedules SET running_at = NULL,
    next_run_at = CASE WHEN next_run_at IS NOT NULL AND next_run_at <= ? THEN ? ELSE next_run_at END
WHERE user_id = ? AND id = ?`, ts(now), nullTS(next), userID, id)
	if err != nil {
		return fmt.Errorf("release schedule: %w", err)
	}
	return nil
}

// Disable turns the schedule off for reason, as loom's own decision.
func (st *Store) Disable(ctx context.Context, userID, id, reason string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `
UPDATE schedules SET enabled = 0, next_run_at = NULL, disabled_reason = ?, disabled_at = ?, updated_at = datetime('now')
WHERE user_id = ? AND id = ?`, reason, ts(at), userID, id)
	if err != nil {
		return fmt.Errorf("disable schedule: %w", err)
	}
	return nil
}

// ResetInterrupted clears every claim and fails every run left running: at
// boot nothing runs yet, so they are what a previous process left behind.
// It returns the threads those runs wrote into, so the caller can close each
// with a note.
func (st *Store) ResetInterrupted(ctx context.Context) ([]InterruptedRun, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT user_id, thread_id FROM schedule_runs
WHERE status = 'running' AND thread_id IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list interrupted runs: %w", err)
	}
	var out []InterruptedRun
	for rows.Next() {
		var r InterruptedRun
		if err := rows.Scan(&r.UserID, &r.ThreadID); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan interrupted run: %w", err)
		}
		out = append(out, r)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE schedules SET running_at = NULL WHERE running_at IS NOT NULL`); err != nil {
		return nil, fmt.Errorf("reset schedule claims: %w", err)
	}
	if _, err := st.db.ExecContext(ctx, `
UPDATE schedule_runs SET status = 'failed', error = 'interrupted by a restart', finished_at = datetime('now')
WHERE status = 'running'`); err != nil {
		return nil, fmt.Errorf("fail interrupted runs: %w", err)
	}
	return out, nil
}

// InterruptedRun is the thread a run left behind when the process stopped.
type InterruptedRun struct {
	UserID   string
	ThreadID string
}

// StartRun records a run that begins now.
func (st *Store) StartRun(ctx context.Context, userID, scheduleID string, at time.Time) (string, error) {
	id := sqlutil.NewID()
	_, err := st.db.ExecContext(ctx, `INSERT INTO schedule_runs (id, schedule_id, user_id, started_at) VALUES (?, ?, ?, ?)`,
		id, scheduleID, userID, ts(at))
	if err != nil {
		return "", fmt.Errorf("start run: %w", err)
	}
	return id, nil
}

// AttachRunThread records the thread a run started.
func (st *Store) AttachRunThread(ctx context.Context, userID, runID, threadID string) error {
	_, err := st.db.ExecContext(ctx, `UPDATE schedule_runs SET thread_id = ? WHERE user_id = ? AND id = ?`, threadID, userID, runID)
	if err != nil {
		return fmt.Errorf("attach run thread: %w", err)
	}
	return nil
}

// RunningInThread reports whether a run of the user's is still writing into
// threadID.
func (st *Store) RunningInThread(ctx context.Context, userID, threadID string) (bool, error) {
	var running bool
	err := st.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM schedule_runs WHERE user_id = ? AND thread_id = ? AND status = 'running')`,
		userID, threadID).Scan(&running)
	if err != nil {
		return false, fmt.Errorf("running in thread: %w", err)
	}
	return running, nil
}

// FinishRun writes a finished run's outcome.
func (st *Store) FinishRun(ctx context.Context, userID string, r Run, at time.Time) error {
	if r.NewLabels == nil {
		r.NewLabels = []string{}
	}
	labels, err := json.Marshal(r.NewLabels)
	if err != nil {
		return fmt.Errorf("encode run labels: %w", err)
	}
	_, err = st.db.ExecContext(ctx, `
UPDATE schedule_runs
SET message_id = ?, finished_at = ?, status = ?, new_count = ?, labels = ?, rounds = ?, tokens = ?, cost_nano_usd = ?, duration_ms = ?, error = ?
WHERE user_id = ? AND id = ?`,
		r.MessageID, ts(at), r.Status, r.NewCount, labels, r.Rounds, r.Tokens, r.CostNanoUSD, r.DurationMS, r.Error, userID, r.ID)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

const runColumns = "id, schedule_id, thread_id, message_id, started_at, finished_at, status, new_count, labels, rounds, tokens, cost_nano_usd, duration_ms, error"

func scanRun(row rowScanner) (Run, error) {
	var r Run
	var threadID, messageID, finishedAt sql.NullString
	var startedAt, labels string
	if err := row.Scan(&r.ID, &r.ScheduleID, &threadID, &messageID, &startedAt, &finishedAt, &r.Status, &r.NewCount, &labels, &r.Rounds, &r.Tokens, &r.CostNanoUSD, &r.DurationMS, &r.Error); err != nil {
		return Run{}, err
	}
	r.ThreadID, r.MessageID = nullString(threadID), nullString(messageID)
	var err error
	if r.StartedAt, err = sqlutil.ParseTime(startedAt); err != nil {
		return Run{}, err
	}
	if r.FinishedAt, err = parseNull(finishedAt); err != nil {
		return Run{}, err
	}
	if err := json.Unmarshal([]byte(labels), &r.NewLabels); err != nil {
		return Run{}, fmt.Errorf("decode run labels: %w", err)
	}
	return r, nil
}

// ListRuns returns the schedule's latest runs, newest first.
func (st *Store) ListRuns(ctx context.Context, userID, scheduleID string, limit int) ([]Run, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+runColumns+` FROM schedule_runs WHERE user_id = ? AND schedule_id = ? ORDER BY started_at DESC, rowid DESC LIMIT ?`,
		userID, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	runs := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, r)
	}
	_ = rows.Close()
	return runs, rows.Err()
}

// lastAnsweredRunStart is when the schedule's latest run that reported
// (news, nothing new or as much as the token limit allowed) started; zero
// before any.
func (st *Store) lastAnsweredRunStart(ctx context.Context, userID, scheduleID string) (time.Time, error) {
	var started string
	err := st.db.QueryRowContext(ctx, `
SELECT started_at FROM schedule_runs
WHERE user_id = ? AND schedule_id = ? AND status IN (?, ?, ?)
ORDER BY started_at DESC, rowid DESC LIMIT 1`, userID, scheduleID, StatusNew, StatusNothingNew, StatusTokenLimit).Scan(&started)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("last answered run: %w", err)
	}
	return sqlutil.ParseTime(started)
}

// LastRuns returns each of the user's schedules' latest run, by schedule id.
func (st *Store) LastRuns(ctx context.Context, userID string) (map[string]Run, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT `+runColumns+` FROM schedule_runs r
WHERE r.user_id = ? AND r.rowid = (
    SELECT l.rowid FROM schedule_runs l
    WHERE l.user_id = r.user_id AND l.schedule_id = r.schedule_id
    ORDER BY l.started_at DESC, l.rowid DESC LIMIT 1)`, userID)
	if err != nil {
		return nil, fmt.Errorf("last runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out[r.ScheduleID] = r
	}
	return out, rows.Err()
}

// DeleteSources forgets one learned source, or all of them when urlKey is "".
func (st *Store) DeleteSources(ctx context.Context, userID, scheduleID, urlKey string) error {
	return st.deleteMemory(ctx, "schedule_sources", "url_key", userID, scheduleID, urlKey)
}

// DeleteQueries forgets one search, or all of them when query is "".
func (st *Store) DeleteQueries(ctx context.Context, userID, scheduleID, query string) error {
	return st.deleteMemory(ctx, "schedule_queries", "query", userID, scheduleID, query)
}

func (st *Store) deleteMemory(ctx context.Context, table, keyColumn, userID, scheduleID, key string) error {
	query := `DELETE FROM ` + table + ` WHERE user_id = ? AND schedule_id = ?`
	args := []any{userID, scheduleID}
	if key != "" {
		query += ` AND ` + keyColumn + ` = ?`
		args = append(args, key)
	}
	if _, err := st.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete from %s: %w", table, err)
	}
	return nil
}

// ListSources returns the schedule's learned sources, most finds first.
func (st *Store) ListSources(ctx context.Context, userID, scheduleID string) ([]Source, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT url_key, prefer, fetch_failed_at, browser_ok_at, runs, finds, last_used_at FROM schedule_sources
WHERE user_id = ? AND schedule_id = ? ORDER BY finds DESC, runs DESC, last_used_at DESC, url_key`, userID, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Source{}
	for rows.Next() {
		var s Source
		var failed, ok sql.NullString
		var used string
		if err := rows.Scan(&s.URLKey, &s.Prefer, &failed, &ok, &s.Runs, &s.Finds, &used); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		if s.FetchFailedAt, err = parseNull(failed); err != nil {
			return nil, err
		}
		if s.BrowserOKAt, err = parseNull(ok); err != nil {
			return nil, err
		}
		if s.LastUsedAt, err = sqlutil.ParseTime(used); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListQueries returns the schedule's remembered searches, most finds first.
func (st *Store) ListQueries(ctx context.Context, userID, scheduleID string) ([]Query, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT query, runs, finds, empty_runs, last_used_at FROM schedule_queries
WHERE user_id = ? AND schedule_id = ? ORDER BY finds DESC, last_used_at DESC, query`, userID, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("list queries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Query{}
	for rows.Next() {
		var q Query
		var used string
		if err := rows.Scan(&q.Query, &q.Runs, &q.Finds, &q.EmptyRuns, &used); err != nil {
			return nil, fmt.Errorf("scan query: %w", err)
		}
		if q.LastUsedAt, err = sqlutil.ParseTime(used); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// CreateNotification adds an item to the user's inbox.
func (st *Store) CreateNotification(ctx context.Context, userID string, n Notification) error {
	_, err := st.db.ExecContext(ctx, `
INSERT INTO notifications (id, user_id, kind, schedule_id, thread_id, message_id, title, body)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, sqlutil.NewID(), userID, n.Kind, n.ScheduleID, n.ThreadID, n.MessageID, n.Title, n.Body)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

// ListNotifications returns the user's latest notifications, newest first.
func (st *Store) ListNotifications(ctx context.Context, userID string, limit int) ([]Notification, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT id, kind, schedule_id, thread_id, message_id, title, body, created_at, read_at FROM notifications
WHERE user_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var scheduleID, threadID, messageID, readAt sql.NullString
		var created string
		if err := rows.Scan(&n.ID, &n.Kind, &scheduleID, &threadID, &messageID, &n.Title, &n.Body, &created, &readAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		n.ScheduleID, n.ThreadID, n.MessageID = nullString(scheduleID), nullString(threadID), nullString(messageID)
		if n.CreatedAt, err = sqlutil.ParseTime(created); err != nil {
			return nil, err
		}
		if n.ReadAt, err = parseNull(readAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount is how many of the user's notifications are unread.
func (st *Store) UnreadCount(ctx context.Context, userID string) (int, error) {
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// MarkRead marks one notification read, or all of the user's when id is "".
func (st *Store) MarkRead(ctx context.Context, userID, id string) error {
	if id == "" {
		return st.markRead(ctx, userID, "")
	}
	return st.markRead(ctx, userID, ` AND id = ?`, id)
}

// MarkThreadRead marks the run news pointing at a thread read; a notice that
// the schedule was disabled stays for the bell.
func (st *Store) MarkThreadRead(ctx context.Context, userID, threadID string) error {
	return st.markRead(ctx, userID, ` AND thread_id = ? AND kind = ?`, threadID, KindScheduleRun)
}

// markRead marks the user's unread notifications matching the extra clause.
func (st *Store) markRead(ctx context.Context, userID, clause string, args ...any) error {
	if _, err := st.db.ExecContext(ctx, `UPDATE notifications SET read_at = datetime('now') WHERE user_id = ? AND read_at IS NULL`+clause,
		append([]any{userID}, args...)...); err != nil {
		return fmt.Errorf("mark notifications read: %w", err)
	}
	return nil
}

// Prune drops notifications older than notificationRetention and learned
// sources and searches unused for memoryRetention, across all users.
func (st *Store) Prune(ctx context.Context, now time.Time) error {
	for _, stmt := range []struct {
		query string
		age   time.Duration
	}{
		{`DELETE FROM notifications WHERE created_at < ?`, notificationRetention},
		{`DELETE FROM schedule_runs WHERE started_at < ? AND status != 'running'`, runRetention},
		{`DELETE FROM schedule_sources WHERE last_used_at < ?`, memoryRetention},
		{`DELETE FROM schedule_queries WHERE last_used_at < ?`, memoryRetention},
	} {
		if _, err := st.db.ExecContext(ctx, stmt.query, ts(now.Add(-stmt.age))); err != nil {
			return fmt.Errorf("prune: %w", err)
		}
	}
	return nil
}

const (
	notificationRetention = 30 * 24 * time.Hour
	memoryRetention       = 30 * 24 * time.Hour
	// runRetention keeps half a year of run history.
	runRetention = 180 * 24 * time.Hour
)

// trimText cuts s to at most n runes on a rune boundary.
func trimText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}
