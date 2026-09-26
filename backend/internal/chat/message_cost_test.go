package chat

import (
	"context"
	"testing"
)

// AddMessageCost adds a late cost (a call that finished after the message was
// written) onto that message, user-scoped, so the thread's Σ counts it.
func TestStore_AddMessageCostAddsToTheMessageUserScoped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	owner := insertTestUser(t, db, "owner")
	other := insertTestUser(t, db, "other")
	s := NewStore(db)
	thread, err := s.CreateThread(ctx, owner, CreateThreadInput{})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	cost := int64(100)
	msg, err := s.AddMessageWithUsage(ctx, owner, thread.ID, RoleAssistant, "answer", MessageTokenUsage{CostNanoUSD: &cost})
	if err != nil {
		t.Fatalf("AddMessageWithUsage: %v", err)
	}
	unpriced, err := s.AddMessage(ctx, owner, thread.ID, RoleUser, "question")
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if ok, err := s.AddMessageCost(ctx, other, msg.ID, 50); err != nil || ok {
		t.Fatalf("other user's AddMessageCost = %v, %v; want false, nil", ok, err)
	}
	if ok, err := s.AddMessageCost(ctx, owner, msg.ID, 50); err != nil || !ok {
		t.Fatalf("AddMessageCost = %v, %v; want true, nil", ok, err)
	}
	if ok, err := s.AddMessageCost(ctx, owner, unpriced.ID, 7); err != nil || !ok {
		t.Fatalf("AddMessageCost on an unpriced message = %v, %v; want true, nil", ok, err)
	}

	got, _, err := s.getMessage(ctx, owner, msg.ID)
	if err != nil {
		t.Fatalf("getMessage: %v", err)
	}
	if got.CostNanoUSD == nil || *got.CostNanoUSD != 150 {
		t.Fatalf("cost = %v, want 150", got.CostNanoUSD)
	}
	got, _, err = s.getMessage(ctx, owner, unpriced.ID)
	if err != nil {
		t.Fatalf("getMessage: %v", err)
	}
	if got.CostNanoUSD == nil || *got.CostNanoUSD != 7 {
		t.Fatalf("cost on the unpriced message = %v, want 7", got.CostNanoUSD)
	}
}
