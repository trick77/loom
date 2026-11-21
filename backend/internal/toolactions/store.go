package toolactions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// ErrNotFound is an action the user does not have.
var ErrNotFound = errors.New("tool action not found")

// ErrNotPending is an action the user has that is no longer waiting for the
// click: claimed, settled, dismissed or past its retention. The call returns
// it as it stands.
var ErrNotPending = errors.New("tool action not pending")

// Retention is how long a proposal waits for its click. Past it the card
// says it is gone: an old call's arguments are not what the user wants now.
const Retention = 30 * 24 * time.Hour

const retentionSQL = "-30 days"

// DBTX is the subset of *sql.DB the store uses.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store keeps tool actions. Every query is scoped by user_id.
type Store struct {
	db DBTX
}

// NewStore returns a store on db.
func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

// Expired reports whether a proposal is past its retention.
func (a Action) Expired() bool {
	return a.State == StateProposed && time.Since(a.CreatedAt) > Retention
}

const columns = `id, user_id, thread_id, tool, arguments, state, result, created_at, decided_at`

type rowScanner interface{ Scan(...any) error }

func scan(row rowScanner) (Action, error) {
	var a Action
	var arguments string
	var result, decidedAt sql.NullString
	var createdAt string
	if err := row.Scan(&a.ID, &a.UserID, &a.ThreadID, &a.Tool, &arguments, &a.State, &result, &createdAt, &decidedAt); err != nil {
		return Action{}, err
	}
	a.Arguments = json.RawMessage(arguments)
	if result.Valid {
		var r Result
		if err := json.Unmarshal([]byte(result.String), &r); err != nil {
			return Action{}, fmt.Errorf("decode result: %w", err)
		}
		a.Result = &r
	}
	var err error
	if a.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return Action{}, err
	}
	if decidedAt.Valid {
		t, err := sqlutil.ParseTime(decidedAt.String)
		if err != nil {
			return Action{}, err
		}
		a.DecidedAt = &t
	}
	return a, nil
}

// Propose records a call the turn showed as a card. The thread must be the
// user's (the foreign key pairs user and thread).
func (s *Store) Propose(ctx context.Context, userID, threadID, tool string, arguments json.RawMessage) (Action, error) {
	if !json.Valid(arguments) {
		return Action{}, errors.New("propose tool action: arguments are not JSON")
	}
	id := sqlutil.NewID()
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO tool_actions (id, user_id, thread_id, tool, arguments, state) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, threadID, tool, string(arguments), StateProposed); err != nil {
		return Action{}, fmt.Errorf("propose tool action: %w", err)
	}
	return s.get(ctx, userID, id)
}

func (s *Store) get(ctx context.Context, userID, id string) (Action, error) {
	a, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM tool_actions WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Action{}, ErrNotFound
	}
	if err != nil {
		return Action{}, fmt.Errorf("get tool action: %w", err)
	}
	return a, nil
}

// GetMany loads the user's actions by id; unknown ids are absent.
func (s *Store) GetMany(ctx context.Context, userID string, ids []string) (map[string]Action, error) {
	out := make(map[string]Action, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM tool_actions WHERE user_id = ? AND id IN (`+sqlutil.Placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("get tool actions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tool action: %w", err)
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

// transition moves an action from proposed (within its retention) or
// running to state in one statement, so two clicks never both win. An
// action in any other state comes back as it is with ErrNotPending.
func (s *Store) transition(ctx context.Context, userID, id, from, to string, result *Result) (Action, error) {
	var encoded sql.NullString
	if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return Action{}, fmt.Errorf("encode result: %w", err)
		}
		encoded = sql.NullString{String: string(raw), Valid: true}
	}
	fresh := "1 = 1"
	if from == StateProposed {
		fresh = "created_at >= datetime('now', '" + retentionSQL + "')"
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE tool_actions SET state = ?, result = COALESCE(?, result), decided_at = datetime('now')
WHERE user_id = ? AND id = ? AND state = ? AND `+fresh, to, encoded, userID, id, from)
	if err != nil {
		return Action{}, fmt.Errorf("update tool action: %w", err)
	}
	a, err := s.get(ctx, userID, id)
	if err != nil {
		return Action{}, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return a, ErrNotPending
	}
	return a, nil
}

// Claim takes a pending proposal for the click that runs it.
func (s *Store) Claim(ctx context.Context, userID, id string) (Action, error) {
	return s.transition(ctx, userID, id, StateProposed, StateRunning, nil)
}

// Finish settles a claimed action as done or failed with its result.
func (s *Store) Finish(ctx context.Context, userID, id, state string, result Result) (Action, error) {
	if state != StateDone && state != StateFailed {
		return Action{}, fmt.Errorf("finish tool action: state %q", state)
	}
	return s.transition(ctx, userID, id, StateRunning, state, &result)
}

// Dismiss discards a pending proposal: its card's "Dismiss".
func (s *Store) Dismiss(ctx context.Context, userID, id string) (Action, error) {
	return s.transition(ctx, userID, id, StateProposed, StateDismissed, nil)
}
