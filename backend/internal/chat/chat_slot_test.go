package chat

import (
	"context"
	"testing"
)

// A thread starts on the slot it was created with (standard when none),
// keeps it across reads, and switches it only when an update names one.
func TestThreadChatSlot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	plain, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Plain"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.ChatSlot != ChatSlotStandard {
		t.Fatalf("new thread slot = %q, want %q", plain.ChatSlot, ChatSlotStandard)
	}
	deep, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Deep", ChatSlot: "deep"})
	if err != nil {
		t.Fatal(err)
	}
	if deep.ChatSlot != "deep" {
		t.Fatalf("created slot = %q, want deep", deep.ChatSlot)
	}

	title := "Renamed"
	got, ok, err := store.UpdateThread(ctx, userID, deep.ID, UpdateThreadInput{Title: &title})
	if err != nil || !ok || got.ChatSlot != "deep" {
		t.Fatalf("rename: slot %q ok %v err %v, want deep kept", got.ChatSlot, ok, err)
	}
	standard := ChatSlotStandard
	got, ok, err = store.UpdateThread(ctx, userID, deep.ID, UpdateThreadInput{ChatSlot: &standard})
	if err != nil || !ok || got.ChatSlot != ChatSlotStandard || got.Title != title {
		t.Fatalf("switch: %+v ok %v err %v, want standard and the title kept", got, ok, err)
	}

	other := insertTestUser(t, db, "bob")
	if _, ok, err := store.UpdateThread(ctx, other, deep.ID, UpdateThreadInput{ChatSlot: &standard}); err != nil || ok {
		t.Fatalf("another user's update: ok %v err %v, want not found", ok, err)
	}
}
