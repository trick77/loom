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

func TestStore_CountAndListUserMessages(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// A projectless thread and a project thread both count toward user messages.
	loose, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Loose"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Trip"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	bound, err := store.CreateThread(ctx, userID, CreateThreadInput{ProjectID: &project.ID, Title: "Flights"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, loose.ID, RoleUser, "I live in Zurich"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, bound.ID, RoleAssistant, "Noted."); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}

	count, err := store.CountUserMessages(ctx, userID)
	if err != nil {
		t.Fatalf("CountUserMessages() error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (across project and projectless threads)", count)
	}

	messages, err := store.ListUserMessages(ctx, userID, 200)
	if err != nil {
		t.Fatalf("ListUserMessages() error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	// Chronological order.
	if messages[0].Content != "I live in Zurich" || messages[1].Content != "Noted." {
		t.Fatalf("messages out of order: %q, %q", messages[0].Content, messages[1].Content)
	}

	// Cap is honored (most recent kept).
	capped, err := store.ListUserMessages(ctx, userID, 1)
	if err != nil {
		t.Fatalf("ListUserMessages(limit 1) error: %v", err)
	}
	if len(capped) != 1 || capped[0].Content != "Noted." {
		t.Fatalf("capped = %+v, want only the most recent message", capped)
	}
}
