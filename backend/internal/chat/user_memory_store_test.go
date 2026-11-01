package chat

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStore_UpsertUserMemoryTruncatesOnRuneBoundary(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Multi-byte runes that exceed the cap; byte-slicing would split one.
	oversized := strings.Repeat("é", MaxUserMemoryLength+50)
	memory, err := store.UpsertUserMemory(ctx, userID, oversized, 1)
	if err != nil {
		t.Fatalf("UpsertUserMemory() error: %v", err)
	}
	if !utf8.ValidString(memory.Content) {
		t.Fatalf("stored content is not valid UTF-8")
	}
	if got := utf8.RuneCountInString(memory.Content); got != MaxUserMemoryLength {
		t.Fatalf("rune count = %d, want %d", got, MaxUserMemoryLength)
	}
}

func TestStore_UserMemoryLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// No memory yet.
	if _, found, err := store.GetUserMemory(ctx, userID); err != nil || found {
		t.Fatalf("GetUserMemory() initial = found %v, err %v; want not found", found, err)
	}

	memory, err := store.UpsertUserMemory(ctx, userID, "- Works at Acme", 4)
	if err != nil {
		t.Fatalf("UpsertUserMemory() error: %v", err)
	}
	if memory.Content != "- Works at Acme" || memory.SourceMessageCount != 4 {
		t.Fatalf("memory = %+v, want content/count set", memory)
	}
	if memory.UpdatedAt == nil {
		t.Fatalf("memory.UpdatedAt = nil, want set")
	}

	// Upsert overwrites (re-summarize, not append).
	updated, err := store.UpsertUserMemory(ctx, userID, "- Works at Globex", 8)
	if err != nil {
		t.Fatalf("second UpsertUserMemory() error: %v", err)
	}
	if updated.Content != "- Works at Globex" || updated.SourceMessageCount != 8 {
		t.Fatalf("updated memory = %+v, want overwritten", updated)
	}

	got, found, err := store.GetUserMemory(ctx, userID)
	if err != nil || !found {
		t.Fatalf("GetUserMemory() = found %v, err %v; want found", found, err)
	}
	if got.Content != "- Works at Globex" {
		t.Fatalf("got.Content = %q, want overwritten content", got.Content)
	}
}
