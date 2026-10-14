package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

type queueFixture struct {
	db       *sql.DB
	store    *Store
	userID   string
	threadID string
}

func newQueueFixture(t *testing.T) queueFixture {
	t.Helper()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(context.Background(), userID, CreateThreadInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return queueFixture{db: db, store: store, userID: userID, threadID: thread.ID}
}

func TestQueuedMessages_EnqueueListsInOrderScopedByUser(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	bobID := insertTestUser(t, f.db, "bob")

	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "first", nil, "send-1"); err != nil {
		t.Fatalf("EnqueueMessage() error = %v", err)
	}
	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "second", json.RawMessage(`[{"id":"p1","label":"Pasted","content":"x"}]`), "send-2"); err != nil {
		t.Fatalf("EnqueueMessage() error = %v", err)
	}

	queued, err := f.store.ListQueued(ctx, f.userID, f.threadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 2 || queued[0].Content != "first" || queued[1].Content != "second" {
		t.Fatalf("ListQueued() = %+v, want first, second", queued)
	}
	if queued[1].ClientMessageID != "send-2" || string(queued[1].PastedTexts) == "[]" {
		t.Fatalf("queued[1] = %+v, want client id and pasted texts", queued[1])
	}

	other, err := f.store.ListQueued(ctx, bobID, f.threadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("ListQueued(bob) = %d rows, want 0", len(other))
	}
	if _, err := f.store.EnqueueMessage(ctx, bobID, f.threadID, "intrude", nil, "send-3"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("EnqueueMessage(bob) error = %v, want ErrThreadNotFound", err)
	}
}

func TestQueuedMessages_RejectsEmptyContentAndDuplicateSend(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "  ", nil, "send-1"); err == nil {
		t.Fatal("EnqueueMessage(empty) error = nil, want validation error")
	}
	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "a", nil, "send-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "a again", nil, "send-1"); err == nil {
		t.Fatal("EnqueueMessage(duplicate send id) error = nil, want an error")
	}
}

func TestQueuedMessages_ConsumeMovesRowsIntoTranscriptOnce(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	if _, err := f.store.AddMessage(ctx, f.userID, f.threadID, RoleUser, "question"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ content, id string }{{"one", "send-1"}, {"two", "send-2"}} {
		if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, c.content, nil, c.id); err != nil {
			t.Fatal(err)
		}
	}
	// The segment row is written after the messages were queued; the consumed
	// rows must still come after it.
	if _, err := f.store.AddMessageWithCitations(ctx, f.userID, f.threadID, RoleAssistant, "", MessageTokenUsage{}, nil, nil, nil, json.RawMessage(`[{"type":"trace","events":[]}]`)); err != nil {
		t.Fatalf("segment insert error = %v", err)
	}

	consumed, err := f.store.ConsumeQueued(ctx, f.userID, f.threadID)
	if err != nil {
		t.Fatalf("ConsumeQueued() error = %v", err)
	}
	if len(consumed) != 2 || consumed[0].Content != "one" || consumed[1].ClientMessageID != "send-2" || consumed[0].Role != RoleUser {
		t.Fatalf("ConsumeQueued() = %+v", consumed)
	}
	again, err := f.store.ConsumeQueued(ctx, f.userID, f.threadID)
	if err != nil || len(again) != 0 {
		t.Fatalf("second ConsumeQueued() = %d rows, err %v; want 0", len(again), err)
	}
	messages, _, err := f.store.ListMessages(ctx, f.userID, f.threadID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range messages {
		order = append(order, string(m.Role)+":"+m.Content)
	}
	want := []string{"user:question", "assistant:", "user:one", "user:two"}
	if len(order) != len(want) {
		t.Fatalf("transcript = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("transcript = %v, want %v", order, want)
		}
	}
}

func TestQueuedMessages_DeleteAndDrain(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	for _, id := range []string{"send-1", "send-2"} {
		if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "text "+id, nil, id); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := f.store.DeleteQueued(ctx, f.userID, f.threadID, "send-1")
	if err != nil || !deleted {
		t.Fatalf("DeleteQueued() = %v, %v; want true", deleted, err)
	}
	if deleted, _ := f.store.DeleteQueued(ctx, f.userID, f.threadID, "send-1"); deleted {
		t.Fatal("DeleteQueued(again) = true, want false")
	}
	drained, err := f.store.DrainQueued(ctx, f.userID, f.threadID)
	if err != nil || len(drained) != 1 || drained[0].ClientMessageID != "send-2" {
		t.Fatalf("DrainQueued() = %+v, %v", drained, err)
	}
	if left, _ := f.store.ListQueued(ctx, f.userID, f.threadID); len(left) != 0 {
		t.Fatalf("after drain %d rows left", len(left))
	}
}

func TestQueuedMessages_ThreadsWithQueuedAndCascade(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	if _, err := f.store.EnqueueMessage(ctx, f.userID, f.threadID, "pending", nil, "send-1"); err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.ThreadsWithQueued(ctx)
	if err != nil || len(pending) != 1 || pending[0].UserID != f.userID || pending[0].ThreadID != f.threadID {
		t.Fatalf("ThreadsWithQueued() = %+v, %v", pending, err)
	}
	if _, err := f.store.DeleteThread(ctx, f.userID, f.threadID); err != nil {
		t.Fatal(err)
	}
	pending, err = f.store.ThreadsWithQueued(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("after thread delete ThreadsWithQueued() = %+v, %v", pending, err)
	}
}

func TestStore_AssistantRowNeedsContentOrBlocks(t *testing.T) {
	ctx := context.Background()
	f := newQueueFixture(t)
	if _, err := f.store.AddMessageWithCitations(ctx, f.userID, f.threadID, RoleAssistant, "", MessageTokenUsage{}, nil, nil, nil, nil); err == nil {
		t.Fatal("empty assistant without blocks: error = nil, want validation error")
	}
	if _, err := f.store.AddMessage(ctx, f.userID, f.threadID, RoleUser, ""); err == nil {
		t.Fatal("empty user message: error = nil, want validation error")
	}
}
