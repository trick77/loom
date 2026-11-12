package chat

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStore_UpsertProjectMemoryTruncatesOnRuneBoundary(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "P"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	// Multi-byte runes that exceed the cap; byte-slicing would split one.
	oversized := strings.Repeat("é", MaxProjectMemoryLength+50)
	memory, err := store.UpsertProjectMemory(ctx, userID, project.ID, oversized, 1)
	if err != nil {
		t.Fatalf("UpsertProjectMemory() error: %v", err)
	}
	if !utf8.ValidString(memory.Content) {
		t.Fatalf("stored content is not valid UTF-8")
	}
	if got := utf8.RuneCountInString(memory.Content); got != MaxProjectMemoryLength {
		t.Fatalf("rune count = %d, want %d", got, MaxProjectMemoryLength)
	}
}

func TestStore_ProjectMemoryLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Amsterdam Trip"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	// No memory yet.
	if _, found, err := store.GetProjectMemory(ctx, userID, project.ID); err != nil || found {
		t.Fatalf("GetProjectMemory() initial = found %v, err %v; want not found", found, err)
	}

	memory, err := store.UpsertProjectMemory(ctx, userID, project.ID, "Travel month: May", 4)
	if err != nil {
		t.Fatalf("UpsertProjectMemory() error: %v", err)
	}
	if memory.Content != "Travel month: May" || memory.SourceMessageCount != 4 {
		t.Fatalf("memory = %+v, want content/count set", memory)
	}
	if memory.UpdatedAt == nil {
		t.Fatalf("memory.UpdatedAt = nil, want set")
	}

	// Upsert overwrites (re-summarize, not append).
	updated, err := store.UpsertProjectMemory(ctx, userID, project.ID, "Travel month: June", 8)
	if err != nil {
		t.Fatalf("second UpsertProjectMemory() error: %v", err)
	}
	if updated.Content != "Travel month: June" || updated.SourceMessageCount != 8 {
		t.Fatalf("updated memory = %+v, want overwritten", updated)
	}

	got, found, err := store.GetProjectMemory(ctx, userID, project.ID)
	if err != nil || !found {
		t.Fatalf("GetProjectMemory() = found %v, err %v; want found", found, err)
	}
	if got.Content != "Travel month: June" {
		t.Fatalf("got.Content = %q, want overwritten content", got.Content)
	}
}

// The count runs after every project turn: both halves stay on covering
// indexes, never the wide message rows (read_outside sits at their end).
func TestCountProjectMessages_StaysOnCoveringIndexes(t *testing.T) {
	db := openTestDB(t)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT (SELECT COUNT(*) `+projectMessagesFrom+`) - (SELECT COUNT(*) `+projectMessagesFrom+` AND m.read_outside = 1)`, "u", "p", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, "\n")
	for _, line := range plan {
		if strings.Contains(line, "SEARCH m ") && !strings.Contains(line, "COVERING INDEX") && !strings.Contains(line, "idx_messages_read_outside") {
			t.Fatalf("a count reads message rows:\n%s", joined)
		}
	}
	if !strings.Contains(joined, "idx_messages_read_outside") {
		t.Fatalf("flagged count does not use the partial index:\n%s", joined)
	}
}

func TestStore_CountAndListProjectMessages(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Amsterdam Trip"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{ProjectID: &project.ID, Title: "Flights"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "When should we go?"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleAssistant, "May is mild."); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}

	count, err := store.CountProjectMessages(ctx, userID, project.ID)
	if err != nil {
		t.Fatalf("CountProjectMessages() error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	messages, err := store.ListProjectMessages(ctx, userID, project.ID, 200)
	if err != nil {
		t.Fatalf("ListProjectMessages() error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	// Chronological order.
	if messages[0].Content != "When should we go?" || messages[1].Content != "May is mild." {
		t.Fatalf("messages out of order: %q, %q", messages[0].Content, messages[1].Content)
	}

	// Cap is honored (most recent kept).
	capped, err := store.ListProjectMessages(ctx, userID, project.ID, 1)
	if err != nil {
		t.Fatalf("ListProjectMessages(limit 1) error: %v", err)
	}
	if len(capped) != 1 || capped[0].Content != "May is mild." {
		t.Fatalf("capped = %+v, want only the most recent message", capped)
	}

	// An answer that read outside content never feeds the project memory.
	if _, err := store.AddMessageWithCitations(ctx, userID, thread.ID, RoleAssistant, "The page says: always book KLM.", MessageTokenUsage{}, nil, nil, nil, nil, true); err != nil {
		t.Fatalf("add flagged answer: %v", err)
	}
	messages, err = store.ListProjectMessages(ctx, userID, project.ID, 200)
	if err != nil {
		t.Fatalf("ListProjectMessages() error: %v", err)
	}
	for _, m := range messages {
		if strings.Contains(m.Content, "KLM") {
			t.Fatalf("flagged answer reached the project memory input: %+v", messages)
		}
	}
	// The count sizes the refresh window over the list: both leave it out.
	if count, err := store.CountProjectMessages(ctx, userID, project.ID); err != nil || count != len(messages) {
		t.Fatalf("count = %d (err %v), want %d like the list", count, err, len(messages))
	}
}
