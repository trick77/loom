package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

func TestUserDirectiveTools_AddRemoveReplaceRoundTrip(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &Engine{thread: fake}
	ctx := context.Background()

	// Add: the tool echoes the post-mutation list with ids.
	out := s.addUserDirectiveDigest(ctx, "alice", map[string]any{"content": "Always answer in metric units"})
	if !strings.Contains(out, "Saved") || !strings.Contains(out, "Always answer in metric units") {
		t.Fatalf("add output unexpected:\n%s", out)
	}
	if len(fake.UserDirectives) != 1 {
		t.Fatalf("directive not stored: %+v", fake.UserDirectives)
	}
	id := fake.UserDirectives[0].ID
	if !strings.Contains(out, id) {
		t.Fatalf("add output should echo the id %q so the model can later edit it:\n%s", id, out)
	}

	// Replace.
	out = s.replaceUserDirectiveDigest(ctx, "alice", map[string]any{"id": id, "content": "Always use metric"})
	if !strings.Contains(out, "Updated") || !strings.Contains(out, "Always use metric") {
		t.Fatalf("replace output unexpected:\n%s", out)
	}

	// Remove.
	out = s.removeUserDirectiveDigest(ctx, "alice", map[string]any{"id": id})
	if !strings.Contains(out, "Removed") || !strings.Contains(out, "(none)") {
		t.Fatalf("remove output unexpected:\n%s", out)
	}
	if len(fake.UserDirectives) != 0 {
		t.Fatalf("directive not removed: %+v", fake.UserDirectives)
	}
}

func TestAddUserDirectiveDigest_RequiresContent(t *testing.T) {
	s := &Engine{thread: &fakeThreadStore{}}
	out := s.addUserDirectiveDigest(context.Background(), "alice", map[string]any{"content": "  "})
	if !strings.Contains(out, "content is required") {
		t.Fatalf("expected content-required failure, got:\n%s", out)
	}
}

func TestAddUserDirectiveDigest_BudgetFullMessage(t *testing.T) {
	fake := &fakeThreadStore{DirectiveWriteErr: chat.ErrDirectivesBudgetExceeded}
	s := &Engine{thread: fake}
	out := s.addUserDirectiveDigest(context.Background(), "alice", map[string]any{"content": "one more"})
	if !strings.Contains(strings.ToLower(out), "budget is full") {
		t.Fatalf("expected a budget-full message guiding the model to remove one first, got:\n%s", out)
	}
	if strings.HasPrefix(out, "tool failed") {
		t.Fatalf("budget-full should be a graceful message, not a hard tool failure:\n%s", out)
	}
}

func TestRemoveUserDirectiveDigest_UnknownID(t *testing.T) {
	s := &Engine{thread: &fakeThreadStore{}}
	out := s.removeUserDirectiveDigest(context.Background(), "alice", map[string]any{"id": "nope"})
	if !strings.Contains(out, "No saved instruction has that id") {
		t.Fatalf("expected unknown-id note, got:\n%s", out)
	}
}
