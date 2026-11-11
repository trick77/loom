package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

func TestUserFactsTool_AddUpdateForget(t *testing.T) {
	fake := &fakeThreadStore{}
	s := &Engine{thread: fake}
	ctx := context.Background()

	out := s.userFactsDigest(ctx, "alice", "thr_1", map[string]any{"action": "add", "category": "Personal", "content": "Lives Bern", "long_term": true})
	if !strings.Contains(out, "Saved") || len(fake.Facts) != 1 || !fake.Facts[0].Durable || fake.Facts[0].Category != "personal" {
		t.Fatalf("add: out %q, facts %+v", out, fake.Facts)
	}
	id := fake.Facts[0].ID
	if !strings.Contains(out, id) {
		t.Fatalf("add output should echo the id %q:\n%s", id, out)
	}

	out = s.userFactsDigest(ctx, "alice", "thr_1", map[string]any{"action": "update", "id": id, "content": "Lives Zurich"})
	if !strings.Contains(out, "Updated") || fake.Facts[0].Content != "Lives Zurich" {
		t.Fatalf("update: out %q, facts %+v", out, fake.Facts)
	}
	out = s.userFactsDigest(ctx, "alice", "thr_1", map[string]any{"action": "update", "id": "ghost", "content": "x"})
	if !strings.Contains(out, "No saved fact") || !strings.Contains(out, id) {
		t.Fatalf("update of an unknown id should list the current facts:\n%s", out)
	}

	s.userFactsDigest(ctx, "alice", "thr_1", map[string]any{"action": "add", "category": "work", "content": "Works at Acme"})
	out = s.userFactsDigest(ctx, "alice", "thr_1", map[string]any{"action": "remove", "ids": []any{id, fake.Facts[1].ID, "ghost", id}})
	if !strings.Contains(out, "Forgot 2 fact(s)") || !strings.Contains(out, "Lives Zurich") || !strings.Contains(out, "Works at Acme") || !strings.Contains(out, "1 id(s) matched no saved fact") {
		t.Fatalf("forget output must list exactly what went:\n%s", out)
	}
	if len(fake.Facts) != 0 {
		t.Fatalf("facts left after forget: %+v", fake.Facts)
	}
}

func TestUserFactsTool_RefusalsAreToolOutput(t *testing.T) {
	fake := &fakeThreadStore{FactWriteErr: chat.ErrUserFactInvalid}
	s := &Engine{thread: fake}
	ctx := context.Background()
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no action", map[string]any{"content": "x"}, "action must be add, update or remove"},
		{"unknown action", map[string]any{"action": "delete", "ids": []any{"x"}}, "action must be add, update or remove"},
		{"invalid add", map[string]any{"action": "add", "category": "history", "content": "x"}, "Could not save"},
		{"update without id", map[string]any{"action": "update", "content": "x"}, "both id and content are required"},
		{"empty remove", map[string]any{"action": "remove", "ids": []any{}}, "ids is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if out := s.userFactsDigest(ctx, "alice", "", tc.args); !strings.Contains(out, tc.want) {
				t.Fatalf("output %q, want it to contain %q", out, tc.want)
			}
		})
	}
	// forget is remove's other name: the user says "forget …".
	ok := &Engine{thread: &fakeThreadStore{}}
	if out := ok.userFactsDigest(ctx, "alice", "", map[string]any{"action": "forget", "ids": []any{"ghost"}}); !strings.Contains(out, "None of those ids") {
		t.Fatalf("forget of unknown ids: %q", out)
	}
}

// Each memory tool replaced three with the default cap apiece: one round
// still runs as many calls.
func TestMemoryToolsCapPerRound(t *testing.T) {
	e := &Engine{}
	for _, name := range []string{UserDirectivesToolName, UserFactsToolName} {
		if got := e.toolCallCapPerRound(name); got != 3*maxToolCallsPerRound {
			t.Errorf("%s cap = %d, want %d", name, got, 3*maxToolCallsPerRound)
		}
	}
}

// Each tool routes the other layer's requests to the other tool, and forget
// keeps what it promises the user.
func TestMemoryTools_DescriptionsSeparateFactsFromDirectives(t *testing.T) {
	facts := userFactsTool().Function.Description
	if !strings.Contains(facts, UserDirectivesToolName) || !strings.Contains(facts, "how you should respond") {
		t.Fatalf("%s must route response preferences to the directive tool:\n%s", UserFactsToolName, facts)
	}
	directives := userDirectivesTool().Function.Description
	if !strings.Contains(directives, UserFactsToolName) {
		t.Fatalf("%s must route facts about the user to the fact tool:\n%s", UserDirectivesToolName, directives)
	}
	for _, want := range []string{"EVERY fact", "long-term", "does not delete the chat threads"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("%s description missing %q:\n%s", UserFactsToolName, want, facts)
		}
	}
}

// The two tools ride in every turn: together they stay well under the six
// tools they replaced (~4.9 KB).
func TestMemoryToolsSchemaSize(t *testing.T) {
	total := 0
	for _, tool := range []llm.Tool{userDirectivesTool(), userFactsTool()} {
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		total += len(raw)
	}
	t.Logf("memory tools: %d bytes", total)
	if total > 2800 {
		t.Errorf("memory tools = %d bytes, want at most 2800", total)
	}
}
