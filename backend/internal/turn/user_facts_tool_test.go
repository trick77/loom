package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

func TestUserFactTools_RememberUpdateForget(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &Engine{thread: fake}
	ctx := context.Background()

	out := s.addUserFactDigest(ctx, "alice", "thr_1", map[string]any{"category": "Personal", "content": "Lives Bern", "long_term": true})
	if !strings.Contains(out, "Saved") || len(fake.Facts) != 1 || !fake.Facts[0].Durable || fake.Facts[0].Category != "personal" {
		t.Fatalf("remember: out %q, facts %+v", out, fake.Facts)
	}
	id := fake.Facts[0].ID
	if !strings.Contains(out, id) {
		t.Fatalf("remember output should echo the id %q:\n%s", id, out)
	}

	out = s.updateUserFactDigest(ctx, "alice", "thr_1", map[string]any{"id": id, "content": "Lives Zurich"})
	if !strings.Contains(out, "Updated") || fake.Facts[0].Content != "Lives Zurich" {
		t.Fatalf("update: out %q, facts %+v", out, fake.Facts)
	}
	out = s.updateUserFactDigest(ctx, "alice", "thr_1", map[string]any{"id": "ghost", "content": "x"})
	if !strings.Contains(out, "No saved fact") || !strings.Contains(out, id) {
		t.Fatalf("update of an unknown id should list the current facts:\n%s", out)
	}

	s.addUserFactDigest(ctx, "alice", "thr_1", map[string]any{"category": "work", "content": "Works at Acme"})
	out = s.forgetUserFactDigest(ctx, "alice", map[string]any{"ids": []any{id, fake.Facts[1].ID, "ghost"}})
	if !strings.Contains(out, "Forgot 2 fact(s)") || !strings.Contains(out, "Lives Zurich") || !strings.Contains(out, "Works at Acme") || !strings.Contains(out, "1 id(s) matched no saved fact") {
		t.Fatalf("forget output must list exactly what went:\n%s", out)
	}
	if len(fake.Facts) != 0 {
		t.Fatalf("facts left after forget: %+v", fake.Facts)
	}
}

func TestUserFactTools_RefusalsAreToolOutput(t *testing.T) {
	fake := &fakeThreadStore{FactWriteErr: chat.ErrUserFactInvalid}
	s := &Engine{thread: fake}
	ctx := context.Background()
	if out := s.addUserFactDigest(ctx, "alice", "", map[string]any{"category": "history", "content": "x"}); !strings.Contains(out, "Could not save") {
		t.Fatalf("invalid add: %q", out)
	}
	if out := s.forgetUserFactDigest(ctx, "alice", map[string]any{"ids": []any{}}); !strings.Contains(out, "ids is required") {
		t.Fatalf("empty forget: %q", out)
	}
	if out := s.forgetUserFactDigest(ctx, "alice", map[string]any{"ids": []any{"ghost"}}); !strings.Contains(out, "None of those ids") {
		t.Fatalf("forget of unknown ids: %q", out)
	}
}

func TestUserFactTools_DescriptionsSeparateFactsFromDirectives(t *testing.T) {
	remember := addUserFactTool().Function.Description
	if !strings.Contains(remember, addUserDirectiveToolName) || !strings.Contains(remember, "how you should respond") {
		t.Fatalf("remember_user_fact must route response preferences to the directive tool:\n%s", remember)
	}
	directive := addUserDirectiveTool().Function.Description
	if !strings.Contains(directive, addUserFactToolName) {
		t.Fatalf("remember_user_directive must route facts about the user to the fact tool:\n%s", directive)
	}
	forget := forgetUserFactTool().Function.Description
	for _, want := range []string{"EVERY fact", "long-term", "does not delete the chat threads"} {
		if !strings.Contains(forget, want) {
			t.Fatalf("forget_user_fact description missing %q:\n%s", want, forget)
		}
	}
}
