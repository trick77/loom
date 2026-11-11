package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

func TestUserDirectivesTool_AddUpdateRemoveRoundTrip(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &Engine{thread: fake}
	ctx := context.Background()

	// Add: the tool echoes the post-mutation list with ids.
	out := s.userDirectivesDigest(ctx, "alice", map[string]any{"action": "add", "content": "Always answer in metric units"})
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

	out = s.userDirectivesDigest(ctx, "alice", map[string]any{"action": "update", "id": id, "content": "Always use metric"})
	if !strings.Contains(out, "Updated") || !strings.Contains(out, "Always use metric") {
		t.Fatalf("update output unexpected:\n%s", out)
	}

	out = s.userDirectivesDigest(ctx, "alice", map[string]any{"action": " Remove ", "id": id})
	if !strings.Contains(out, "Removed") || !strings.Contains(out, "(none)") {
		t.Fatalf("remove output unexpected:\n%s", out)
	}
	if len(fake.UserDirectives) != 0 {
		t.Fatalf("directive not removed: %+v", fake.UserDirectives)
	}
}

// "forget that" reaches either layer: forget removes a directive too.
func TestUserDirectivesTool_ForgetRemoves(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &Engine{thread: fake}
	ctx := context.Background()
	s.userDirectivesDigest(ctx, "alice", map[string]any{"action": "add", "content": "Call me Al"})
	out := s.userDirectivesDigest(ctx, "alice", map[string]any{"action": "forget", "id": fake.UserDirectives[0].ID})
	if !strings.Contains(out, "Removed") || len(fake.UserDirectives) != 0 {
		t.Fatalf("forget: out %q, directives %+v", out, fake.UserDirectives)
	}
}

func TestUserDirectivesTool_Refusals(t *testing.T) {
	s := &Engine{thread: &fakeThreadStore{}}
	ctx := context.Background()
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no action", map[string]any{"content": "x"}, "action must be add, update or remove"},
		{"unknown action", map[string]any{"action": "delete", "id": "x"}, "action must be add, update or remove"},
		{"add without content", map[string]any{"action": "add", "content": "  "}, "content is required"},
		{"update without id", map[string]any{"action": "update", "content": "x"}, "both id and content are required"},
		{"remove without id", map[string]any{"action": "remove"}, "id is required"},
		{"remove of an unknown id", map[string]any{"action": "remove", "id": "nope"}, "No saved instruction has that id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if out := s.userDirectivesDigest(ctx, "alice", tc.args); !strings.Contains(out, tc.want) {
				t.Fatalf("output %q, want it to contain %q", out, tc.want)
			}
		})
	}
}

func TestUserDirectivesTool_BudgetFullMessage(t *testing.T) {
	fake := &fakeThreadStore{DirectiveWriteErr: chat.ErrDirectivesBudgetExceeded}
	s := &Engine{thread: fake}
	out := s.userDirectivesDigest(context.Background(), "alice", map[string]any{"action": "add", "content": "one more"})
	if !strings.Contains(strings.ToLower(out), "budget is full") {
		t.Fatalf("expected a budget-full message guiding the model to remove one first, got:\n%s", out)
	}
	if strings.HasPrefix(out, "tool failed") {
		t.Fatalf("budget-full should be a graceful message, not a hard tool failure:\n%s", out)
	}
}
