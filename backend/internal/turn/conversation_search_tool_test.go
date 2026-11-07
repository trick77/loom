package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

func TestConversationSearchDigest_FormatsHits(t *testing.T) {
	fake := &fakeThreadStore{SearchHits: []chat.MessageSearchHit{
		{
			MessageID:   "m1",
			ThreadID:    "thread-abc",
			ThreadTitle: "Sharing design",
			Role:        chat.RoleAssistant,
			Snippet:     "We «froze» the «snapshot» so later edits never leak into a shared link.",
			// 22:30 UTC is already the next day in Zurich: a hit's date reads
			// in the user's zone, as the prompt's date line does.
			CreatedAt: time.Date(2026, 5, 31, 22, 30, 0, 0, time.UTC),
		},
	}}
	s := &Engine{thread: fake}

	out := s.conversationSearchDigest(context.Background(), "alice", chat.Thread{ID: "current"}, map[string]any{
		"query": "frozen snapshots",
	}, zoneLocation("Europe/Zurich"))

	for _, want := range []string{"Sharing design", "thread-abc", "2026-06-01", "read_thread", "«froze» the «snapshot»"} {
		if !strings.Contains(out, want) {
			t.Fatalf("digest missing %q:\n%s", want, out)
		}
	}
}

func TestConversationSearchDigest_NoMatches(t *testing.T) {
	fake := &fakeThreadStore{SearchHits: nil}
	s := &Engine{thread: fake}

	out := s.conversationSearchDigest(context.Background(), "alice", chat.Thread{ID: "current"}, map[string]any{
		"query": "nonexistent",
	}, time.UTC)
	if !strings.Contains(out, "No messages") || !strings.Contains(out, "nonexistent") {
		t.Fatalf("expected a no-match note, got:\n%s", out)
	}
}

func TestConversationSearchDigest_RequiresQuery(t *testing.T) {
	s := &Engine{thread: &fakeThreadStore{}}
	out := s.conversationSearchDigest(context.Background(), "alice", chat.Thread{ID: "current"}, map[string]any{
		"query": "   ",
	}, time.UTC)
	if !strings.Contains(out, "query is required") {
		t.Fatalf("expected query-required failure, got:\n%s", out)
	}
}
