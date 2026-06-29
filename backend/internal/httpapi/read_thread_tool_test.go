package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

func TestReadThreadDigest_OwnedThread(t *testing.T) {
	fake := &fakeThreadStore{
		thread: chat.Thread{ID: "thread-abc", Title: "Sharing design"},
		messages: []chat.Message{
			{ID: "m1", Role: chat.RoleUser, Content: "Why frozen snapshots?"},
			{ID: "m2", Role: chat.RoleAssistant, Content: "So edits never leak into a shared link."},
		},
	}
	s := &server{thread: fake, projectSummaryTokenBudget: 4000}

	out := s.readThreadDigest(context.Background(), "alice", "thread-abc")
	for _, want := range []string{"=== Thread: Sharing design ===", "Why frozen snapshots?", "leak into a shared link"} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript missing %q:\n%s", want, out)
		}
	}
}

func TestReadThreadDigest_NotOwnedReturnsNotFound(t *testing.T) {
	// Empty fake.thread.ID makes the user-scoped GetThread report not-found, the
	// same way the real store does for another user's (or a missing) thread id.
	s := &server{thread: &fakeThreadStore{}, projectSummaryTokenBudget: 4000}

	out := s.readThreadDigest(context.Background(), "alice", "someone-elses-thread")
	if !strings.Contains(out, "No thread with id") {
		t.Fatalf("expected not-found note, got:\n%s", out)
	}
	if strings.Contains(out, "=== Thread") {
		t.Fatalf("must not render a transcript for an unowned thread:\n%s", out)
	}
}

func TestReadThreadDigest_RequiresThreadID(t *testing.T) {
	s := &server{thread: &fakeThreadStore{}, projectSummaryTokenBudget: 4000}
	out := s.readThreadDigest(context.Background(), "alice", "  ")
	if !strings.Contains(out, "thread_id is required") {
		t.Fatalf("expected thread_id-required failure, got:\n%s", out)
	}
}
