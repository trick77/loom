package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/store"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertTestUser(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES (?, ?, ?, 'user')`,
		id, "subject-"+id, id,
	)
	if err != nil {
		t.Fatalf("insert user %s: %v", id, err)
	}
	return id
}

func ptr(value int) *int {
	return &value
}

func strptr(value string) *string {
	return &value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func TestStore_CreateProjectAndThreadScopesByUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	aliceID := insertTestUser(t, db, "alice")
	bobID := insertTestUser(t, db, "bob")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, aliceID, CreateProjectInput{
		Name:        "  Alice Project  ",
		Description: "  Project notes  ",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.Name != "Alice Project" {
		t.Fatalf("project.Name = %q, want trimmed name", project.Name)
	}
	if project.Description != "Project notes" {
		t.Fatalf("project.Description = %q, want trimmed description", project.Description)
	}

	thread, err := store.CreateThread(ctx, aliceID, CreateThreadInput{
		ProjectID: &project.ID,
		Title:     "  Planning  ",
	})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if thread.ProjectID == nil || *thread.ProjectID != project.ID {
		t.Fatalf("thread.ProjectID = %v, want %q", thread.ProjectID, project.ID)
	}
	if thread.Title != "Planning" {
		t.Fatalf("thread.Title = %q, want trimmed title", thread.Title)
	}

	if _, ok, err := store.GetThread(ctx, bobID, thread.ID); err != nil {
		t.Fatalf("Bob GetThread() error: %v", err)
	} else if ok {
		t.Fatal("Bob GetThread() ok = true, want false")
	}

	got, ok, err := store.GetThread(ctx, aliceID, thread.ID)
	if err != nil {
		t.Fatalf("Alice GetThread() error: %v", err)
	}
	if !ok {
		t.Fatal("Alice GetThread() ok = false, want true")
	}
	if got.ProjectID == nil || *got.ProjectID != project.ID {
		t.Fatalf("got.ProjectID = %v, want %q", got.ProjectID, project.ID)
	}
}

func TestStore_SetProjectDescriptionIfEmptyIsOneShot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	updated, changed, err := store.SetProjectDescriptionIfEmpty(ctx, userID, project.ID, "  Early research plan.  ")
	if err != nil {
		t.Fatalf("SetProjectDescriptionIfEmpty() error: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true for initially empty description")
	}
	if updated.Description != "Early research plan." {
		t.Fatalf("Description = %q, want trimmed generated description", updated.Description)
	}

	again, changed, err := store.SetProjectDescriptionIfEmpty(ctx, userID, project.ID, "Replacement")
	if err != nil {
		t.Fatalf("second SetProjectDescriptionIfEmpty() error: %v", err)
	}
	if changed {
		t.Fatal("changed = true, want false after auto-description marker is set")
	}
	if again.Description != "Early research plan." {
		t.Fatalf("Description after second attempt = %q, want original", again.Description)
	}
}

func TestStore_ListMethodsReturnEmptySlices(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	projects, err := store.ListProjects(ctx, userID, false)
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if projects == nil {
		t.Fatal("ListProjects() = nil, want empty slice")
	}
	if len(projects) != 0 {
		t.Fatalf("len(projects) = %d, want 0", len(projects))
	}

	threads, err := store.ListThreads(ctx, userID, ListThreadsOptions{})
	if err != nil {
		t.Fatalf("ListThreads() error: %v", err)
	}
	if threads == nil {
		t.Fatal("ListThreads() = nil, want empty slice")
	}
	if len(threads) != 0 {
		t.Fatalf("len(threads) = %d, want 0", len(threads))
	}

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Empty messages"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	messages, ok, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if !ok {
		t.Fatal("ListMessages() ok = false, want true")
	}
	if messages == nil {
		t.Fatal("ListMessages() = nil, want empty slice")
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(messages))
	}
}

func TestMessagesPersistArtifacts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Artifacts"})
	if err != nil {
		t.Fatal(err)
	}

	rawArtifacts := json.RawMessage(`[{"id":"art_1","displayFilename":"report.pdf","downloadUrl":"/api/artifacts/art_1/download"}]`)
	message, err := store.AddMessageWithArtifacts(ctx, userID, thread.ID, RoleAssistant, "Created report.pdf", MessageTokenUsage{}, rawArtifacts)
	if err != nil {
		t.Fatalf("AddMessageWithArtifacts() error = %v", err)
	}
	if string(message.Artifacts) != string(rawArtifacts) {
		t.Fatalf("message.Artifacts = %s", message.Artifacts)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	if string(messages[0].Artifacts) != string(rawArtifacts) {
		t.Fatalf("listed Artifacts = %s", messages[0].Artifacts)
	}
}

func TestMessagesPersistActivityTrace(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Activity"})
	if err != nil {
		t.Fatal(err)
	}

	rawTrace := json.RawMessage(`[{"id":"reasoning-1","type":"reasoning","content":"I searched first.","status":"done"},{"id":"call_1","type":"tool","name":"search__web","status":"done","rawArguments":"{\"q\":\"slopr\"}","rawOutput":"search result"}]`)
	message, err := store.AddMessageWithActivityTrace(ctx, userID, thread.ID, RoleAssistant, "I found Slopr.", MessageTokenUsage{}, nil, rawTrace)
	if err != nil {
		t.Fatalf("AddMessageWithActivityTrace() error = %v", err)
	}
	if string(message.ActivityTrace) != string(rawTrace) {
		t.Fatalf("message.ActivityTrace = %s", message.ActivityTrace)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	if string(messages[0].ActivityTrace) != string(rawTrace) {
		t.Fatalf("listed ActivityTrace = %s", messages[0].ActivityTrace)
	}
}

func TestMessagesPersistAttachments(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Attachments"})
	if err != nil {
		t.Fatal(err)
	}

	rawAttachments := json.RawMessage(`[{"kind":"image","artifactId":"art_1","filename":"photo.png","mimeType":"image/png","sizeBytes":1234,"downloadUrl":"/api/artifacts/art_1/download"}]`)
	message, err := store.AddMessageWithAttachments(ctx, userID, thread.ID, RoleUser, "Look at this", rawAttachments)
	if err != nil {
		t.Fatalf("AddMessageWithAttachments() error = %v", err)
	}
	if string(message.Attachments) != string(rawAttachments) {
		t.Fatalf("message.Attachments = %s", message.Attachments)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	if string(messages[0].Attachments) != string(rawAttachments) {
		t.Fatalf("listed Attachments = %s", messages[0].Attachments)
	}
}

func TestMessagesDefaultAttachmentsToEmptyArray(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Plain"})
	if err != nil {
		t.Fatal(err)
	}

	message, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "no attachments")
	if err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	if string(message.Attachments) != "[]" {
		t.Fatalf("message.Attachments = %s, want []", message.Attachments)
	}
}

func TestMessagesPersistCitations(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Citations"})
	if err != nil {
		t.Fatal(err)
	}

	rawCitations := json.RawMessage(`[{"documentId":"d1","filename":"guide.pdf","snippet":"make build","score":0.8}]`)
	message, err := store.AddMessageWithCitations(ctx, userID, thread.ID, RoleAssistant, "Use make build.", MessageTokenUsage{}, nil, nil, rawCitations)
	if err != nil {
		t.Fatalf("AddMessageWithCitations() error = %v", err)
	}
	if string(message.Citations) != string(rawCitations) {
		t.Fatalf("message.Citations = %s", message.Citations)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !found {
		t.Fatalf("ListMessages() found=%v err=%v", found, err)
	}
	if string(messages[0].Citations) != string(rawCitations) {
		t.Fatalf("listed Citations = %s", messages[0].Citations)
	}
}

func TestStore_AddMessageRollsBackWhenThreadTimestampUpdateFails(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Atomic"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TRIGGER fail_thread_timestamp_update
BEFORE UPDATE OF last_message_at ON threads
BEGIN
	SELECT RAISE(ABORT, 'timestamp update failed');
END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "hello"); err == nil {
		t.Fatal("AddMessage() error = nil, want timestamp update failure")
	}

	messages, ok, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if !ok {
		t.Fatal("ListMessages() ok = false, want true")
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want rollback to leave 0", len(messages))
	}
}

func TestStore_AddMessageWithUsagePersistsTokenStats(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Usage"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	message, err := store.AddMessageWithUsage(ctx, userID, thread.ID, RoleAssistant, "answer", MessageTokenUsage{
		PromptTokens:     ptr(7),
		CompletionTokens: ptr(3),
		TotalTokens:      ptr(10),
		CachedTokens:     ptr(5),
		ReasoningTokens:  ptr(2),
		ReasoningEffort:  strptr("high"),
	})
	if err != nil {
		t.Fatalf("AddMessageWithUsage() error: %v", err)
	}
	if got := intValue(message.PromptTokens); got != 7 {
		t.Fatalf("PromptTokens = %d, want 7", got)
	}
	if got := intValue(message.CompletionTokens); got != 3 {
		t.Fatalf("CompletionTokens = %d, want 3", got)
	}
	if got := intValue(message.TotalTokens); got != 10 {
		t.Fatalf("TotalTokens = %d, want 10", got)
	}
	if got := intValue(message.CachedTokens); got != 5 {
		t.Fatalf("CachedTokens = %d, want 5", got)
	}
	if got := intValue(message.ReasoningTokens); got != 2 {
		t.Fatalf("ReasoningTokens = %d, want 2", got)
	}
	if message.ReasoningEffort == nil || *message.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %v, want \"high\"", message.ReasoningEffort)
	}
}

func TestStore_AddMessageWithUsagePersistsReasoningContent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Reasoning"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	message, err := store.AddMessageWithUsage(ctx, userID, thread.ID, RoleAssistant, "answer", MessageTokenUsage{
		ReasoningContent: "I checked the inputs first.",
	})
	if err != nil {
		t.Fatalf("AddMessageWithUsage() error: %v", err)
	}
	if message.ReasoningContent != "I checked the inputs first." {
		t.Fatalf("message.ReasoningContent = %q", message.ReasoningContent)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if !found || len(messages) != 1 {
		t.Fatalf("messages found=%v len=%d", found, len(messages))
	}
	if messages[0].ReasoningContent != "I checked the inputs first." {
		t.Fatalf("listed reasoning content = %q", messages[0].ReasoningContent)
	}
}

func TestStore_RejectsOverlongInputs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	if _, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: strings.Repeat("p", MaxProjectNameLength+1)}); err == nil {
		t.Fatal("CreateProject() error = nil, want overlong name error")
	}
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Length"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, strings.Repeat("m", MaxMessageContentLength+1)); err == nil {
		t.Fatal("AddMessage() error = nil, want overlong content error")
	}
}

func TestStore_NormalizesThreadTitles(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{
		Title: "  # Albert Einstein 🧠⚛️ The legendary physicist  ",
	})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if thread.Title != "Albert Einstein The legendary physicist" {
		t.Fatalf("thread.Title = %q, want normalized title", thread.Title)
	}

	updatedTitle := ` **Quantum notes** `
	updated, ok, err := store.UpdateThread(ctx, userID, thread.ID, UpdateThreadInput{Title: &updatedTitle})
	if err != nil {
		t.Fatalf("UpdateThread() error: %v", err)
	}
	if !ok {
		t.Fatal("UpdateThread() ok = false, want true")
	}
	if updated.Title != "Quantum notes" {
		t.Fatalf("updated.Title = %q, want normalized markdown title", updated.Title)
	}
}

func TestStore_UpdateThreadProjectMembership(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Notes"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	updated, ok, err := store.UpdateThread(ctx, userID, thread.ID, UpdateThreadInput{
		ProjectID: ProjectIDUpdate{Set: true, Value: &project.ID},
	})
	if err != nil {
		t.Fatalf("UpdateThread(set project) error: %v", err)
	}
	if !ok {
		t.Fatal("UpdateThread(set project) ok = false, want true")
	}
	if updated.ProjectID == nil || *updated.ProjectID != project.ID {
		t.Fatalf("updated.ProjectID = %v, want %q", updated.ProjectID, project.ID)
	}

	projectThreads, err := store.ListThreads(ctx, userID, ListThreadsOptions{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListThreads(project) error: %v", err)
	}
	if len(projectThreads) != 1 || projectThreads[0].ID != thread.ID {
		t.Fatalf("project threads = %#v, want only %q", projectThreads, thread.ID)
	}

	projectlessThreads, err := store.ListThreads(ctx, userID, ListThreadsOptions{ProjectlessOnly: true})
	if err != nil {
		t.Fatalf("ListThreads(projectless) error: %v", err)
	}
	if len(projectlessThreads) != 0 {
		t.Fatalf("projectless thread count = %d, want 0", len(projectlessThreads))
	}

	cleared, ok, err := store.UpdateThread(ctx, userID, thread.ID, UpdateThreadInput{
		ProjectID: ProjectIDUpdate{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("UpdateThread(clear project) error: %v", err)
	}
	if !ok {
		t.Fatal("UpdateThread(clear project) ok = false, want true")
	}
	if cleared.ProjectID != nil {
		t.Fatalf("cleared.ProjectID = %v, want nil", cleared.ProjectID)
	}
}

func TestStore_UpdateThreadRejectsAnotherUsersProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	aliceID := insertTestUser(t, db, "alice")
	bobID := insertTestUser(t, db, "bob")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, aliceID, CreateThreadInput{Title: "Private"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	bobProject, err := store.CreateProject(ctx, bobID, CreateProjectInput{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	_, ok, err := store.UpdateThread(ctx, aliceID, thread.ID, UpdateThreadInput{
		ProjectID: ProjectIDUpdate{Set: true, Value: &bobProject.ID},
	})
	if err == nil {
		t.Fatal("UpdateThread() error = nil, want project not found")
	}
	if ok {
		t.Fatal("UpdateThread() ok = true, want false")
	}
	if err.Error() != "project not found" {
		t.Fatalf("UpdateThread() error = %q, want project not found", err.Error())
	}
}

func TestStore_UpdateThreadTitleAndProjectTogether(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Draft"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	title := "Final notes"

	updated, ok, err := store.UpdateThread(ctx, userID, thread.ID, UpdateThreadInput{
		Title:     &title,
		ProjectID: ProjectIDUpdate{Set: true, Value: &project.ID},
	})
	if err != nil {
		t.Fatalf("UpdateThread() error: %v", err)
	}
	if !ok {
		t.Fatal("UpdateThread() ok = false, want true")
	}
	if updated.Title != "Final notes" {
		t.Fatalf("updated.Title = %q, want Final notes", updated.Title)
	}
	if updated.ProjectID == nil || *updated.ProjectID != project.ID {
		t.Fatalf("updated.ProjectID = %v, want %q", updated.ProjectID, project.ID)
	}
}

func TestStore_KeepsOnlyFirstLineOfMultilineTitles(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Title models sometimes return a full markdown answer instead of a short
	// title: a heading line followed by body paragraphs. Only the first line is
	// a usable title; the body (and its inline markdown) must be dropped.
	title := "Albert Einstein (1879–1955)\n\n**Albert Einstein** was a German-born theoretical physicist"
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: title})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if thread.Title != "Albert Einstein (1879–1955)" {
		t.Fatalf("thread.Title = %q, want first line only", thread.Title)
	}
}

func TestStore_TruncatesNormalizedThreadTitlesWithEllipsis(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: strings.Repeat("t", MaxThreadTitleLength+10)})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	runes := []rune(thread.Title)
	if len(runes) != MaxThreadTitleLength {
		t.Fatalf("len([]rune(thread.Title)) = %d, want %d", len(runes), MaxThreadTitleLength)
	}
	if !strings.HasSuffix(thread.Title, "…") {
		t.Fatalf("thread.Title = %q, want ellipsis suffix", thread.Title)
	}
}

func TestStore_RejectsThreadTitleThatNormalizesToEmpty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Existing"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	title := " # 🧠 "
	if _, _, err := store.UpdateThread(ctx, userID, thread.ID, UpdateThreadInput{Title: &title}); err == nil {
		t.Fatal("UpdateThread() error = nil, want required title error")
	}
}

func TestStore_ListThreadsSupportsRecentsAndStarred(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	first, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "First"})
	if err != nil {
		t.Fatalf("CreateThread(first) error: %v", err)
	}
	second, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Second"})
	if err != nil {
		t.Fatalf("CreateThread(second) error: %v", err)
	}
	if _, ok, err := store.SetThreadStarred(ctx, userID, second.ID, true); err != nil {
		t.Fatalf("SetThreadStarred() error: %v", err)
	} else if !ok {
		t.Fatal("SetThreadStarred() ok = false, want true")
	}

	threads, err := store.ListThreads(ctx, userID, ListThreadsOptions{})
	if err != nil {
		t.Fatalf("ListThreads() error: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("len(threads) = %d, want 2", len(threads))
	}

	starred, err := store.ListThreads(ctx, userID, ListThreadsOptions{StarredOnly: true})
	if err != nil {
		t.Fatalf("ListThreads(starred) error: %v", err)
	}
	if len(starred) != 1 {
		t.Fatalf("len(starred) = %d, want 1", len(starred))
	}
	if starred[0].ID != second.ID {
		t.Fatalf("starred[0].ID = %q, want %q", starred[0].ID, second.ID)
	}
	if !starred[0].Starred {
		t.Fatal("starred[0].Starred = false, want true")
	}
	if first.ID == second.ID {
		t.Fatal("created duplicate thread IDs")
	}
}

func TestStore_ListThreadsSearchFiltersByTitle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	for _, title := range []string{"Greeting", "Morning greeting", "Apps and websites", "100% discount"} {
		if _, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: title}); err != nil {
			t.Fatalf("CreateThread(%q) error: %v", title, err)
		}
	}

	// Case-insensitive substring match on the title.
	matches, err := store.ListThreads(ctx, userID, ListThreadsOptions{Search: "greet"})
	if err != nil {
		t.Fatalf("ListThreads(search greet) error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("len(search greet) = %d, want 2", len(matches))
	}

	// No match returns empty.
	none, err := store.ListThreads(ctx, userID, ListThreadsOptions{Search: "nonexistent"})
	if err != nil {
		t.Fatalf("ListThreads(search nonexistent) error: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("len(search nonexistent) = %d, want 0", len(none))
	}

	// '%' is treated literally (escaped), not as a wildcard.
	literal, err := store.ListThreads(ctx, userID, ListThreadsOptions{Search: "100%"})
	if err != nil {
		t.Fatalf("ListThreads(search 100%%) error: %v", err)
	}
	if len(literal) != 1 || literal[0].Title != "100% discount" {
		t.Fatalf("search '100%%' = %#v, want only '100%% discount'", literal)
	}

	// A lone '%' must not match every thread (would happen without escaping).
	wildcard, err := store.ListThreads(ctx, userID, ListThreadsOptions{Search: "%"})
	if err != nil {
		t.Fatalf("ListThreads(search %%) error: %v", err)
	}
	if len(wildcard) != 1 {
		t.Fatalf("len(search %%) = %d, want 1 (only the literal '100%%' title)", len(wildcard))
	}

	// Whitespace-only search is ignored (returns all).
	all, err := store.ListThreads(ctx, userID, ListThreadsOptions{Search: "   "})
	if err != nil {
		t.Fatalf("ListThreads(search blank) error: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("len(search blank) = %d, want 4", len(all))
	}
}

func TestStore_ArchiveAndUnarchiveThread(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Archive me"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	if ok, err := store.SetThreadArchived(ctx, userID, thread.ID, true); err != nil {
		t.Fatalf("SetThreadArchived(true) error: %v", err)
	} else if !ok {
		t.Fatal("SetThreadArchived(true) ok = false, want true")
	}

	active, err := store.ListThreads(ctx, userID, ListThreadsOptions{})
	if err != nil {
		t.Fatalf("ListThreads(active) error: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("len(active) = %d, want 0", len(active))
	}

	archived, err := store.ListThreads(ctx, userID, ListThreadsOptions{Archived: true})
	if err != nil {
		t.Fatalf("ListThreads(archived) error: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != thread.ID || archived[0].ArchivedAt == nil {
		t.Fatalf("archived threads = %#v, want archived thread %q", archived, thread.ID)
	}

	if ok, err := store.SetThreadArchived(ctx, userID, thread.ID, false); err != nil {
		t.Fatalf("SetThreadArchived(false) error: %v", err)
	} else if !ok {
		t.Fatal("SetThreadArchived(false) ok = false, want true")
	}

	active, err = store.ListThreads(ctx, userID, ListThreadsOptions{})
	if err != nil {
		t.Fatalf("ListThreads(active after unarchive) error: %v", err)
	}
	if len(active) != 1 || active[0].ID != thread.ID || active[0].ArchivedAt != nil {
		t.Fatalf("active threads = %#v, want unarchived thread %q", active, thread.ID)
	}
}

func TestStore_SetProjectStarred(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Research"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.Starred {
		t.Fatal("new project Starred = true, want false")
	}

	starred, ok, err := store.SetProjectStarred(ctx, userID, project.ID, true)
	if err != nil {
		t.Fatalf("SetProjectStarred(true) error: %v", err)
	}
	if !ok {
		t.Fatal("SetProjectStarred(true) ok = false, want true")
	}
	if !starred.Starred {
		t.Fatal("SetProjectStarred(true) returned Starred = false, want true")
	}

	listed, err := store.ListProjects(ctx, userID, false)
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(listed) != 1 || !listed[0].Starred {
		t.Fatalf("listed projects = %#v, want one starred project", listed)
	}

	unstarred, ok, err := store.SetProjectStarred(ctx, userID, project.ID, false)
	if err != nil {
		t.Fatalf("SetProjectStarred(false) error: %v", err)
	}
	if !ok {
		t.Fatal("SetProjectStarred(false) ok = false, want true")
	}
	if unstarred.Starred {
		t.Fatal("SetProjectStarred(false) returned Starred = true, want false")
	}

	if _, ok, err := store.SetProjectStarred(ctx, userID, "missing", true); err != nil {
		t.Fatalf("SetProjectStarred(missing) error: %v", err)
	} else if ok {
		t.Fatal("SetProjectStarred(missing) ok = true, want false")
	}
}

func TestStore_DeleteProjectCascadesThreadsAndMessages(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	project, err := store.CreateProject(ctx, userID, CreateProjectInput{Name: "Project"})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{ProjectID: &project.ID, Title: "Thread"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if _, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "hello"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}

	if ok, err := store.DeleteProject(ctx, userID, project.ID); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	} else if !ok {
		t.Fatal("DeleteProject() ok = false, want true")
	}

	if _, ok, err := store.GetThread(ctx, userID, thread.ID); err != nil {
		t.Fatalf("GetThread() after DeleteProject error: %v", err)
	} else if ok {
		t.Fatal("GetThread() after DeleteProject ok = true, want false")
	}

	if messages, ok, err := store.ListMessages(ctx, userID, thread.ID); err != nil {
		t.Fatalf("ListMessages() after DeleteProject error: %v", err)
	} else if ok || len(messages) != 0 {
		t.Fatalf("ListMessages() after DeleteProject = %d messages, ok %v; want 0, false", len(messages), ok)
	}
}

func TestStore_DeleteThreadCascadesMessagesActivityTraceAndArtifacts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Generated assets"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	trace := json.RawMessage(`[{"id":"call_1","type":"tool","name":"create_pdf_file","status":"done","rawOutput":"created report.pdf"}]`)
	if _, err := store.AddMessageWithActivityTrace(ctx, userID, thread.ID, RoleAssistant, "Created report.pdf", MessageTokenUsage{}, json.RawMessage(`[{"id":"art_1"}]`), trace); err != nil {
		t.Fatalf("AddMessageWithActivityTrace() error: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes)
VALUES ('art_1', ?, ?, 'report.pdf', 'artifacts/report.pdf', 'application/pdf', 42)`, userID, thread.ID); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}

	if ok, err := store.DeleteThread(ctx, userID, thread.ID); err != nil {
		t.Fatalf("DeleteThread() error: %v", err)
	} else if !ok {
		t.Fatal("DeleteThread() ok = false, want true")
	}

	if messages, ok, err := store.ListMessages(ctx, userID, thread.ID); err != nil {
		t.Fatalf("ListMessages() after DeleteThread error: %v", err)
	} else if ok || len(messages) != 0 {
		t.Fatalf("ListMessages() after DeleteThread = %d messages, ok %v; want 0, false", len(messages), ok)
	}
	var artifactCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifacts WHERE user_id = ? AND thread_id = ?`, userID, thread.ID).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != 0 {
		t.Fatalf("artifact count after DeleteThread = %d, want 0", artifactCount)
	}
}

func TestStore_AddMessageUpdatesThreadLastMessageAt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Messages"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if thread.LastMessageAt != nil {
		t.Fatalf("thread.LastMessageAt = %v, want nil", thread.LastMessageAt)
	}

	message, err := store.AddMessage(ctx, userID, thread.ID, RoleUser, "  hello  ")
	if err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if message.Content != "hello" {
		t.Fatalf("message.Content = %q, want trimmed content", message.Content)
	}
	if string(message.ToolCalls) != "[]" {
		t.Fatalf("message.ToolCalls = %q, want []", message.ToolCalls)
	}
	if string(message.Citations) != "[]" {
		t.Fatalf("message.Citations = %q, want []", message.Citations)
	}

	got, ok, err := store.GetThread(ctx, userID, thread.ID)
	if err != nil {
		t.Fatalf("GetThread() error: %v", err)
	}
	if !ok {
		t.Fatal("GetThread() ok = false, want true")
	}
	if got.LastMessageAt == nil {
		t.Fatal("got.LastMessageAt = nil, want timestamp")
	}
	if got.LastMessageAt.Before(thread.UpdatedAt) {
		t.Fatalf("LastMessageAt = %v, want not before original UpdatedAt %v", got.LastMessageAt, thread.UpdatedAt)
	}
}

func TestStore_CreateProjectlessThreadAndListMessagesScopesByUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	aliceID := insertTestUser(t, db, "alice")
	bobID := insertTestUser(t, db, "bob")
	store := NewStore(db)

	thread, err := store.CreateThread(ctx, aliceID, CreateThreadInput{Title: "  "})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}
	if thread.ProjectID != nil {
		t.Fatalf("thread.ProjectID = %v, want nil", thread.ProjectID)
	}
	if thread.Title != DefaultThreadTitle {
		t.Fatalf("thread.Title = %q, want %q", thread.Title, DefaultThreadTitle)
	}

	if _, err := store.AddMessage(ctx, aliceID, thread.ID, RoleUser, "hello"); err != nil {
		t.Fatalf("AddMessage() error: %v", err)
	}
	if messages, ok, err := store.ListMessages(ctx, bobID, thread.ID); err != nil {
		t.Fatalf("Bob ListMessages() error: %v", err)
	} else if ok || len(messages) != 0 {
		t.Fatalf("Bob ListMessages() = %d messages, ok %v; want 0, false", len(messages), ok)
	}

	messages, ok, err := store.ListMessages(ctx, aliceID, thread.ID)
	if err != nil {
		t.Fatalf("Alice ListMessages() error: %v", err)
	}
	if !ok {
		t.Fatal("Alice ListMessages() ok = false, want true")
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if string(messages[0].ToolCalls) != "[]" || string(messages[0].Citations) != "[]" {
		t.Fatalf("message JSON defaults = %q, %q; want [], []", messages[0].ToolCalls, messages[0].Citations)
	}
}

func TestStore_ListThreadsPaginatesWithCursor(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Explicit timestamps make ordering deterministic; t_c uses last_message_at
	// to exercise the COALESCE(last_message_at, updated_at) keyset.
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title, created_at, updated_at, last_message_at)
VALUES ('t_a', ?, 'A', '2026-06-10 09:00:00', '2026-06-10 09:00:00', NULL),
       ('t_b', ?, 'B', '2026-06-10 09:00:01', '2026-06-10 09:00:01', NULL),
       ('t_c', ?, 'C', '2026-06-10 08:00:00', '2026-06-10 08:00:00', '2026-06-10 09:00:05'),
       ('t_d', ?, 'D', '2026-06-10 09:00:03', '2026-06-10 09:00:03', NULL),
       ('t_e', ?, 'E', '2026-06-10 09:00:02', '2026-06-10 09:00:02', NULL)`,
		userID, userID, userID, userID, userID); err != nil {
		t.Fatalf("insert threads: %v", err)
	}

	// Activity DESC: t_c(09:00:05), t_d(09:00:03), t_e(09:00:02), t_b(09:00:01), t_a(09:00:00).
	want := []string{"t_c", "t_d", "t_e", "t_b", "t_a"}

	var got []string
	opts := ListThreadsOptions{Limit: 2}
	limit := EffectiveThreadLimit(opts.Limit)
	for {
		page, err := store.ListThreads(ctx, userID, opts)
		if err != nil {
			t.Fatalf("ListThreads(cursor=%q) error: %v", opts.Cursor, err)
		}
		if len(page) > limit {
			t.Fatalf("page size = %d, want <= %d", len(page), limit)
		}
		for _, thread := range page {
			got = append(got, thread.ID)
		}
		if len(page) < limit {
			break
		}
		opts.Cursor = EncodeThreadCursor(page[len(page)-1])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paged ids = %v, want %v", got, want)
	}

	// ListThreadIDs returns every match in the same order, ignoring limit/cursor.
	ids, err := store.ListThreadIDs(ctx, userID, ListThreadsOptions{})
	if err != nil {
		t.Fatalf("ListThreadIDs() error: %v", err)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ListThreadIDs = %v, want %v", ids, want)
	}

	// Search filter narrows the id set.
	filtered, err := store.ListThreadIDs(ctx, userID, ListThreadsOptions{Search: "C"})
	if err != nil {
		t.Fatalf("ListThreadIDs(search) error: %v", err)
	}
	if len(filtered) != 1 || filtered[0] != "t_c" {
		t.Fatalf("ListThreadIDs(search) = %v, want [t_c]", filtered)
	}
}
