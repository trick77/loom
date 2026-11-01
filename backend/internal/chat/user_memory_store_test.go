package chat

import (
	"context"
	"testing"
)

// The old memory blob is read-only input for the one-time conversion into
// facts: loom no longer writes it.
func TestStore_GetUserMemory(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	if _, found, err := store.GetUserMemory(ctx, userID); err != nil || found {
		t.Fatalf("GetUserMemory() initial = found %v, err %v; want not found", found, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_memory (user_id, content, source_message_count, updated_at) VALUES (?, '- Works at Acme', 4, '2026-10-30 22:43:00')`, userID); err != nil {
		t.Fatalf("seed user_memory: %v", err)
	}
	got, found, err := store.GetUserMemory(ctx, userID)
	if err != nil || !found {
		t.Fatalf("GetUserMemory() = found %v, err %v; want found", found, err)
	}
	if got.Content != "- Works at Acme" || got.SourceMessageCount != 4 || got.UpdatedAt == nil || got.UpdatedAt.Format("2006-01-02 15:04:05") != "2026-10-30 22:43:00" {
		t.Fatalf("got = %+v", got)
	}
}
