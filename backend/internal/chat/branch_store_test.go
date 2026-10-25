package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBranchThreadCopiesTheAnswerAndItsQuestion(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	st := NewStore(db)
	source, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Cinema scout"})
	_, _ = st.AddMessageWithAttachments(ctx, alice, source.ID, RoleUser, "old question", nil, nil, "")
	_, _ = st.AddMessageWithCitations(ctx, alice, source.ID, RoleAssistant, "old answer", MessageTokenUsage{}, nil, nil, nil, nil)
	question, _ := st.AddMessageWithAttachments(ctx, alice, source.ID, RoleUser, "find films", nil, nil, "")
	answer, _ := st.AddMessageWithCitations(ctx, alice, source.ID, RoleAssistant, "Two new films [1]", MessageTokenUsage{TotalTokens: ptr(41000), CostNanoUSD: func() *int64 { v := int64(5); return &v }()}, nil, nil, []byte(`[{"index":1,"url":"https://arena.ch"}]`), nil)

	branch, err := st.BranchThread(ctx, alice, source.ID, answer.ID)
	if err != nil {
		t.Fatalf("BranchThread: %v", err)
	}
	if branch.ID == source.ID || branch.Title != DefaultThreadTitle || branch.ScheduleID != nil {
		t.Fatalf("branch = %+v", branch)
	}
	if branch.BranchedFromThreadID == nil || *branch.BranchedFromThreadID != source.ID || *branch.BranchedFromMessageID != answer.ID {
		t.Fatalf("branch source = %v / %v", branch.BranchedFromThreadID, branch.BranchedFromMessageID)
	}
	messages, _, _ := st.ListMessages(ctx, alice, branch.ID)
	if len(messages) != 2 || messages[0].Content != question.Content || messages[1].Content != answer.Content {
		t.Fatalf("branch messages = %+v", messages)
	}
	if intValue(messages[1].TotalTokens) != 0 || (messages[1].CostNanoUSD != nil && *messages[1].CostNanoUSD != 0) || string(messages[1].Citations) == "[]" {
		t.Fatalf("copied answer = %+v (spend must stay with the original, citations must come along)", messages[1])
	}

	if _, err := st.BranchThread(ctx, bob, source.ID, answer.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("another user's branch err = %v", err)
	}
	if _, err := st.BranchThread(ctx, alice, source.ID, question.ID); !errors.Is(err, ErrNotAnAnswer) {
		t.Fatalf("branch from a question err = %v", err)
	}

	// The branch outlives its source.
	if _, err := st.DeleteThread(ctx, alice, source.ID); err != nil {
		t.Fatal(err)
	}
	kept, ok, _ := st.GetThread(ctx, alice, branch.ID)
	if !ok || kept.BranchedFromThreadID != nil || kept.BranchedFromMessageID != nil {
		t.Fatalf("branch after source delete = %+v, %v", kept, ok)
	}
}

func TestBranchThreadWithoutAQuestion(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	st := NewStore(db)
	source, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "x"})
	answer, _ := st.AddMessageWithCitations(ctx, alice, source.ID, RoleAssistant, "lonely answer", MessageTokenUsage{}, nil, nil, nil, nil)
	branch, err := st.BranchThread(ctx, alice, source.ID, answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	messages, _, _ := st.ListMessages(ctx, alice, branch.ID)
	if len(messages) != 1 || messages[0].Role != RoleAssistant {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestThreadScheduleIDAndSetLastMessageAt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	st := NewStore(db)
	thread, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Cinema scout"})
	if _, err := db.Exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone, thread_id)
VALUES ('s1', ?, 'n', 'p', 'i', 't', 'c', 'UTC', ?)`, alice, thread.ID); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetThread(ctx, alice, thread.ID)
	if got.ScheduleID == nil || *got.ScheduleID != "s1" {
		t.Fatalf("ScheduleID = %v", got.ScheduleID)
	}
	listed, _ := st.ListThreads(ctx, alice, ListThreadsOptions{})
	if len(listed) != 1 || listed[0].ScheduleID == nil {
		t.Fatalf("listed = %+v", listed)
	}

	_, _ = st.AddMessageWithAttachments(ctx, alice, thread.ID, RoleUser, "run", nil, nil, "")
	before := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := st.SetLastMessageAt(ctx, alice, thread.ID, &before); err != nil {
		t.Fatal(err)
	}
	got, _, _ = st.GetThread(ctx, alice, thread.ID)
	if got.LastMessageAt == nil || !got.LastMessageAt.Equal(before) {
		t.Fatalf("last message at = %v", got.LastMessageAt)
	}
	_ = st.SetLastMessageAt(ctx, alice, thread.ID, nil)
	got, _, _ = st.GetThread(ctx, alice, thread.ID)
	if got.LastMessageAt != nil {
		t.Fatalf("cleared last message at = %v", got.LastMessageAt)
	}
}
