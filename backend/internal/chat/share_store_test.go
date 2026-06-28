package chat

import (
	"context"
	"encoding/json"
	"testing"
)

func TestShareStore_lifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Shareable"})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	// No share yet.
	if _, ok, err := store.GetShareByThreadID(ctx, userID, thread.ID); err != nil || ok {
		t.Fatalf("GetShareByThreadID before create: ok=%v err=%v", ok, err)
	}

	share, err := store.CreateShare(ctx, userID, CreateShareInput{
		ShareID:     "tok123",
		ThreadID:    thread.ID,
		Title:       "Shareable",
		Snapshot:    json.RawMessage(`{"messages":[]}`),
		ArtifactIDs: []string{"art1", "art2"},
	})
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if !share.Shared || share.ShareID != "tok123" {
		t.Fatalf("unexpected share: %+v", share)
	}
	if !share.ContainsArtifactID("art1") || share.ContainsArtifactID("nope") {
		t.Fatalf("allowlist wrong: %+v", share.ArtifactIDs)
	}

	// Public lookup by token works and is not user-scoped.
	pub, ok, err := store.GetShareByShareID(ctx, "tok123")
	if err != nil || !ok || pub.ThreadID != thread.ID {
		t.Fatalf("GetShareByShareID: ok=%v err=%v share=%+v", ok, err, pub)
	}

	// Disable => still found by owner, but Shared=false (handler turns this into 404).
	if ok, err := store.SetShareEnabled(ctx, userID, thread.ID, false); err != nil || !ok {
		t.Fatalf("SetShareEnabled: ok=%v err=%v", ok, err)
	}
	if pub, _, _ := store.GetShareByShareID(ctx, "tok123"); pub.Shared {
		t.Fatalf("share should be disabled")
	}

	// Update re-enables and refreshes snapshot.
	updated, ok, err := store.UpdateShareSnapshot(ctx, userID, thread.ID, UpdateShareInput{
		Title:       "Shareable",
		Snapshot:    json.RawMessage(`{"messages":[{"role":"user"}]}`),
		ArtifactIDs: []string{"art3"},
	})
	if err != nil || !ok {
		t.Fatalf("UpdateShareSnapshot: ok=%v err=%v", ok, err)
	}
	if !updated.Shared || !updated.ContainsArtifactID("art3") || updated.ContainsArtifactID("art1") {
		t.Fatalf("update did not refresh: %+v", updated)
	}

	// Listing returns the one share.
	shares, err := store.ListSharesForUser(ctx, userID)
	if err != nil || len(shares) != 1 {
		t.Fatalf("ListSharesForUser: n=%d err=%v", len(shares), err)
	}

	// Deleting the thread cascades the share away (public 404).
	if _, err := store.DeleteThread(ctx, userID, thread.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, ok, _ := store.GetShareByShareID(ctx, "tok123"); ok {
		t.Fatalf("share should have cascaded on thread delete")
	}
}
