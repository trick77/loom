package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

func TestReadThreadDigest_OwnedThread(t *testing.T) {
	// 23:30 UTC is already the next day in Zurich: the activity date is the
	// user's.
	last := time.Date(2026, 6, 1, 23, 30, 0, 0, time.UTC)
	fake := &fakeThreadStore{
		Thread: chat.Thread{ID: "thread-abc", Title: "Sharing design", LastMessageAt: &last},
		Messages: []chat.Message{
			{ID: "m1", Role: chat.RoleUser, Content: "Why frozen snapshots?"},
			{ID: "m2", Role: chat.RoleAssistant, Content: "So edits never leak into a shared link."},
		},
	}
	s := &Engine{thread: fake, projectSummaryTokenBudget: 4000}

	out := s.readThreadDigest(context.Background(), "alice", "thread-abc", zoneLocation("Europe/Zurich"))
	for _, want := range []string{"=== Thread: Sharing design ===", "Last activity: 2026-06-02", "Why frozen snapshots?", "leak into a shared link"} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript missing %q:\n%s", want, out)
		}
	}
}

func TestReadThreadDigest_NotOwnedReturnsNotFound(t *testing.T) {
	// Empty fake.thread.ID makes the user-scoped GetThread report not-found, the
	// same way the real store does for another user's (or a missing) thread id.
	s := &Engine{thread: &fakeThreadStore{}, projectSummaryTokenBudget: 4000}

	out := s.readThreadDigest(context.Background(), "alice", "someone-elses-thread", time.UTC)
	if !strings.Contains(out, "No thread with id") {
		t.Fatalf("expected not-found note, got:\n%s", out)
	}
	if strings.Contains(out, "=== Thread") {
		t.Fatalf("must not render a transcript for an unowned thread:\n%s", out)
	}
}

func TestReadThreadDigest_RequiresThreadID(t *testing.T) {
	s := &Engine{thread: &fakeThreadStore{}, projectSummaryTokenBudget: 4000}
	out := s.readThreadDigest(context.Background(), "alice", "  ", time.UTC)
	if !strings.Contains(out, "thread_id is required") {
		t.Fatalf("expected thread_id-required failure, got:\n%s", out)
	}
}
