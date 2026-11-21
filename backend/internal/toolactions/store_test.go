package toolactions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/store"
)

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1', 's1', 'alice', 'user'), ('u2', 's2', 'bob', 'user')`,
		`INSERT INTO threads (id, user_id, title) VALUES ('t1', 'u1', 'Videos'), ('tb', 'u2', 'Bob')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return NewStore(db), db
}

var args = json.RawMessage(`{"videos":[{"url":"https://youtu.be/Zr7KcW1nB0e","title":"Aero socks"}]}`)

// A proposal is claimed once: the click that runs it, never a second one.
// It settles with its result, which reads back with it.
func TestProposeClaimFinish(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	a, err := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != StateProposed || a.ID == "" || string(a.Arguments) != string(args) {
		t.Fatalf("proposed = %+v", a)
	}
	claimed, err := s.Claim(ctx, "u1", a.ID)
	if err != nil || claimed.State != StateRunning || claimed.Tool != "peeq__add_videos" || string(claimed.Arguments) != string(args) {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	if again, err := s.Claim(ctx, "u1", a.ID); !errors.Is(err, ErrNotPending) || again.State != StateRunning {
		t.Fatalf("second claim = %+v, %v", again, err)
	}
	result := Result{Text: "queued: https://youtu.be/Zr7KcW1nB0e", Items: []mcp.ActionItem{{Status: mcp.ActionDone, Message: "queued"}}, Link: &mcp.ActionLink{URL: "https://peeq.example/activity", Label: "peeq"}}
	done, err := s.Finish(ctx, "u1", a.ID, StateDone, result)
	if err != nil || done.State != StateDone || done.Result == nil || done.Result.Link.Label != "peeq" || done.DecidedAt == nil {
		t.Fatalf("finish = %+v, %v", done, err)
	}
	got, err := s.GetMany(ctx, "u1", []string{a.ID, "nope"})
	if err != nil || len(got) != 1 || got[a.ID].Result.Items[0].Message != "queued" {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

// Every call is the user's own: another user's id is not found, for reads
// and writes alike.
func TestScopedByUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	a, err := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "u2", a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("claim by other = %v", err)
	}
	if _, err := s.Dismiss(ctx, "u2", a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("dismiss by other = %v", err)
	}
	if got, _ := s.GetMany(ctx, "u2", []string{a.ID}); len(got) != 0 {
		t.Errorf("other user read %+v", got)
	}
	if _, err := s.Propose(ctx, "u1", "tb", "peeq__add_videos", args); err == nil {
		t.Error("proposed into another user's thread")
	}
}

// A dismissed proposal can no longer run; a running one can no longer be
// dismissed.
func TestDismiss(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	a, _ := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if d, err := s.Dismiss(ctx, "u1", a.ID); err != nil || d.State != StateDismissed {
		t.Fatalf("dismiss = %+v, %v", d, err)
	}
	if _, err := s.Claim(ctx, "u1", a.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("claim after dismiss = %v", err)
	}
	b, _ := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if _, err := s.Claim(ctx, "u1", b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dismiss(ctx, "u1", b.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("dismiss while running = %v", err)
	}
}

// A proposal past its retention is gone: the click no longer runs it.
func TestExpiredProposalIsNotClaimed(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	a, _ := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if _, err := db.Exec(`UPDATE tool_actions SET created_at = datetime('now', '-31 days') WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "u1", a.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("claim of an expired proposal = %v", err)
	}
	got, _ := s.GetMany(ctx, "u1", []string{a.ID})
	if !got[a.ID].Expired() {
		t.Errorf("expired proposal not reported expired: %+v", got[a.ID])
	}
}

// Deleting the thread takes its proposals with it.
func TestThreadDeleteCascades(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	a, _ := s.Propose(ctx, "u1", "t1", "peeq__add_videos", args)
	if _, err := db.Exec(`DELETE FROM threads WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetMany(ctx, "u1", []string{a.ID}); len(got) != 0 {
		t.Errorf("proposal outlived its thread: %+v", got)
	}
}
