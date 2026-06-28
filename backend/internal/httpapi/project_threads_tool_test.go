package httpapi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/store"
)

func openProjectDigestStore(t *testing.T) (*chat.Store, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const userID = "user_1"
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES (?, ?, ?, 'user')`, userID, "subject", "user"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return chat.NewStore(db), userID
}

func seedThread(t *testing.T, st *chat.Store, userID, projectID, title string, turns [][2]string) chat.Thread {
	t.Helper()
	pid := projectID
	thread, err := st.CreateThread(context.Background(), userID, chat.CreateThreadInput{ProjectID: &pid, Title: title})
	if err != nil {
		t.Fatalf("create thread %q: %v", title, err)
	}
	for _, turn := range turns {
		if _, err := st.AddMessage(context.Background(), userID, thread.ID, chat.Role(turn[0]), turn[1]); err != nil {
			t.Fatalf("add message to %q: %v", title, err)
		}
	}
	return thread
}

// TestProjectThreadsDigestExcludesAndCovers verifies the digest covers every
// active sibling thread, excludes the current thread and archived threads, and
// reads (not just titles) their content.
func TestProjectThreadsDigestExcludesAndCovers(t *testing.T) {
	st, userID := openProjectDigestStore(t)
	ctx := context.Background()
	project, err := st.CreateProject(ctx, userID, chat.CreateProjectInput{Name: "Warsaw City Trip"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	seedThread(t, st, userID, project.ID, "Warsaw Weather", [][2]string{
		{"user", "What's the weather in September?"},
		{"assistant", "Mild, around 15C, pack a light jacket and an umbrella."},
	})
	seedThread(t, st, userID, project.ID, "Warsaw Food", [][2]string{
		{"user", "Where should I eat?"},
		{"assistant", "Try pierogi at Zapiecek and zurek soup near the Old Town."},
	})
	archived := seedThread(t, st, userID, project.ID, "Warsaw Archived", [][2]string{
		{"user", "old question"},
		{"assistant", "OBSOLETE_ANSWER should never appear"},
	})
	if _, err := st.SetThreadArchived(ctx, userID, archived.ID, true); err != nil {
		t.Fatalf("archive thread: %v", err)
	}
	// The thread the user is asking from — must be excluded from its own digest.
	current := seedThread(t, st, userID, project.ID, "Warsaw Summary Request", [][2]string{
		{"user", "Summarize the threads in this project"},
	})

	srv := &server{thread: st, projectSummaryTokenBudget: 6000}
	digest := srv.projectThreadsDigest(ctx, userID, current)

	for _, want := range []string{"Warsaw Weather", "Warsaw Food", "around 15C", "pierogi at Zapiecek"} {
		if !strings.Contains(digest, want) {
			t.Errorf("digest missing %q\n--- digest ---\n%s", want, digest)
		}
	}
	if strings.Contains(digest, "Warsaw Archived") || strings.Contains(digest, "OBSOLETE_ANSWER") {
		t.Errorf("digest leaked archived thread\n--- digest ---\n%s", digest)
	}
	if strings.Contains(digest, "Warsaw Summary Request") {
		t.Errorf("digest included the current thread\n--- digest ---\n%s", digest)
	}
}

// TestProjectThreadsDigestNoOtherThreads returns a clear message when the project
// has no sibling threads, so the model can say so instead of inventing content.
func TestProjectThreadsDigestNoOtherThreads(t *testing.T) {
	st, userID := openProjectDigestStore(t)
	ctx := context.Background()
	project, err := st.CreateProject(ctx, userID, chat.CreateProjectInput{Name: "Empty"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	current := seedThread(t, st, userID, project.ID, "Only Thread", [][2]string{{"user", "hi"}})

	digest := (&server{thread: st, projectSummaryTokenBudget: 6000}).projectThreadsDigest(ctx, userID, current)
	if !strings.Contains(digest, "no other threads") {
		t.Errorf("expected an explicit no-other-threads message, got:\n%s", digest)
	}
}

// TestProjectThreadsDigestProjectless guards the gate: a thread with no project
// must not attempt to read sibling threads.
func TestProjectThreadsDigestProjectless(t *testing.T) {
	st, userID := openProjectDigestStore(t)
	digest := (&server{thread: st, projectSummaryTokenBudget: 6000}).projectThreadsDigest(context.Background(), userID, chat.Thread{ID: "x"})
	if !strings.Contains(digest, "does not belong to a project") {
		t.Errorf("expected projectless message, got:\n%s", digest)
	}
}

// TestBuildThreadDigestSectionKeepsConclusion is the correctness-critical case:
// under a tight budget the section must keep the FINAL turn (the conclusion),
// not front-truncate to the opening question.
func TestBuildThreadDigestSectionKeepsConclusion(t *testing.T) {
	question := strings.Repeat("question words here ", 200) // large opening turn
	conclusion := "FINAL VERDICT: stay in Srodmiescie."
	messages := []chat.Message{
		{Role: chat.RoleUser, Content: question},
		{Role: chat.RoleAssistant, Content: conclusion},
	}
	// Budget large enough only for the conclusion, not the giant question.
	section := buildThreadDigestSection(messages, 40)
	if !strings.Contains(section, "FINAL VERDICT") {
		t.Errorf("section dropped the conclusion under a tight budget:\n%s", section)
	}
	if strings.Contains(section, "question words here question words") {
		t.Errorf("section kept the bulky opening question instead of the conclusion:\n%s", section)
	}
}

// TestBuildThreadDigestSectionChronologicalOrder confirms kept turns render in
// chronological order even though they're collected newest-first.
func TestBuildThreadDigestSectionChronologicalOrder(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.RoleUser, Content: "AAA first"},
		{Role: chat.RoleAssistant, Content: "BBB second"},
	}
	section := buildThreadDigestSection(messages, 6000)
	if strings.Index(section, "AAA first") > strings.Index(section, "BBB second") {
		t.Errorf("turns not in chronological order:\n%s", section)
	}
}
