package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

func TestUserDirectiveTools_AddRemoveReplaceRoundTrip(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &server{thread: fake}
	ctx := context.Background()

	// Add: the tool echoes the post-mutation list with ids.
	out := s.addUserDirectiveDigest(ctx, "alice", map[string]any{"content": "Always answer in metric units"})
	if !strings.Contains(out, "Saved") || !strings.Contains(out, "Always answer in metric units") {
		t.Fatalf("add output unexpected:\n%s", out)
	}
	if len(fake.userDirectives) != 1 {
		t.Fatalf("directive not stored: %+v", fake.userDirectives)
	}
	id := fake.userDirectives[0].ID
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
	if len(fake.userDirectives) != 0 {
		t.Fatalf("directive not removed: %+v", fake.userDirectives)
	}
}

func TestAddUserDirectiveDigest_RequiresContent(t *testing.T) {
	s := &server{thread: &fakeThreadStore{}}
	out := s.addUserDirectiveDigest(context.Background(), "alice", map[string]any{"content": "  "})
	if !strings.Contains(out, "content is required") {
		t.Fatalf("expected content-required failure, got:\n%s", out)
	}
}

func TestAddUserDirectiveDigest_BudgetFullMessage(t *testing.T) {
	fake := &fakeThreadStore{directiveWriteErr: chat.ErrDirectivesBudgetExceeded}
	s := &server{thread: fake}
	out := s.addUserDirectiveDigest(context.Background(), "alice", map[string]any{"content": "one more"})
	if !strings.Contains(strings.ToLower(out), "budget is full") {
		t.Fatalf("expected a budget-full message guiding the model to remove one first, got:\n%s", out)
	}
	if strings.HasPrefix(out, "tool failed") {
		t.Fatalf("budget-full should be a graceful message, not a hard tool failure:\n%s", out)
	}
}

func TestRemoveUserDirectiveDigest_UnknownID(t *testing.T) {
	s := &server{thread: &fakeThreadStore{}}
	out := s.removeUserDirectiveDigest(context.Background(), "alice", map[string]any{"id": "nope"})
	if !strings.Contains(out, "No saved instruction has that id") {
		t.Fatalf("expected unknown-id note, got:\n%s", out)
	}
}

func TestUserContextForUser_RendersBothBlocksIndependently(t *testing.T) {
	// Directives present but derived memory empty: the directives block must still
	// be injected (the old code returned "" whenever memory was blank).
	fake := &fakeThreadStore{
		userDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Always use metric"}},
	}
	s := &server{thread: fake}
	out := s.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out, "Standing instructions") || !strings.Contains(out, "Always use metric") {
		t.Fatalf("directives block missing when memory is empty:\n%s", out)
	}
	if !strings.Contains(out, "dir_0") {
		t.Fatalf("directive id should be injected so the model can edit it:\n%s", out)
	}
	if strings.Contains(out, "Personal context about the user") {
		t.Fatalf("derived block should be absent when memory is empty:\n%s", out)
	}

	// Derived memory present but no directives: only the derived block.
	fake2 := &fakeThreadStore{userMemory: chat.UserMemory{Content: "## Work context\n- Backend dev"}}
	s2 := &server{thread: fake2}
	out2 := s2.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out2, "Personal context about the user") || !strings.Contains(out2, "Backend dev") {
		t.Fatalf("derived block missing:\n%s", out2)
	}
	if strings.Contains(out2, "Standing instructions") {
		t.Fatalf("directives block should be absent when there are none:\n%s", out2)
	}
}

func TestUserMemoryScope_ExclusionsFeedDirectiveContent(t *testing.T) {
	// The dedup source: the user scope must surface directive CONTENT (no ids) so
	// the generator can avoid restating a standing instruction in derived memory.
	fake := &fakeThreadStore{
		userDirectives: []chat.UserDirective{
			{ID: "dir_0", Content: "Always use metric"},
			{ID: "dir_1", Content: "Call me Jan"},
		},
	}
	s := &server{thread: fake}
	scope := s.userMemoryScope(testUser)
	if scope.exclusions == nil {
		t.Fatal("user scope must set an exclusions hook")
	}
	out, err := scope.exclusions(context.Background())
	if err != nil {
		t.Fatalf("exclusions() error: %v", err)
	}
	if !strings.Contains(out, "Always use metric") || !strings.Contains(out, "Call me Jan") {
		t.Fatalf("exclusions missing directive content:\n%s", out)
	}
	if strings.Contains(out, "dir_0") {
		t.Fatalf("exclusions should carry content only, not ids:\n%s", out)
	}
}

func TestProjectMemoryScope_HasNoExclusions(t *testing.T) {
	// Project memory must be unaffected by the dedup plumbing.
	s := &server{thread: &fakeThreadStore{}}
	scope := s.projectMemoryScope(testUser, chat.Project{ID: "p1", Name: "P"})
	if scope.exclusions != nil {
		t.Fatal("project scope must not set an exclusions hook")
	}
}

func TestUserContextForUser_DirectivesOutrankDerived(t *testing.T) {
	fake := &fakeThreadStore{
		userDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Be terse"}},
		userMemory:     chat.UserMemory{Content: "## Work context\n- Backend dev"},
	}
	s := &server{thread: fake}
	out := s.userContextForUser(context.Background(), "alice")
	di := strings.Index(out, "Standing instructions")
	mi := strings.Index(out, "Personal context about the user")
	if di < 0 || mi < 0 || di > mi {
		t.Fatalf("directives block must come before the derived block (di=%d, mi=%d):\n%s", di, mi, out)
	}
}
