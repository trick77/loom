package artifact

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/store"
)

// TestDetachFromThreadSurvivesThreadDelete pins the mechanism the thread-delete
// path relies on: with thread_id NULL the composite FK to threads no longer
// matches, so the thread's ON DELETE CASCADE leaves the row alone. An artifact
// left attached is taken by the same cascade.
func TestDetachFromThreadSurvivesThreadDelete(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role) VALUES ('user_1','s1','user_1','user'), ('user_2','s2','user_2','user');
INSERT INTO threads (id, user_id, title) VALUES ('thread_1','user_1','T');`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)
	kept, err := s.Create(ctx, CreateInput{UserID: "user_1", ThreadID: "thread_1", DisplayFilename: "shared.txt", VolumeRelPath: "projects/p/shared.txt", MIMEType: "text/plain", SizeBytes: 4})
	if err != nil {
		t.Fatalf("Create kept: %v", err)
	}
	swept, err := s.Create(ctx, CreateInput{UserID: "user_1", ThreadID: "thread_1", DisplayFilename: "chart.png", VolumeRelPath: "files/outputs/chart.png", MIMEType: "image/png", SizeBytes: 4})
	if err != nil {
		t.Fatalf("Create swept: %v", err)
	}

	// An empty id list is a no-op, not an error: the common case is a thread with
	// nothing to spare.
	if err := s.DetachFromThread(ctx, "user_1", nil); err != nil {
		t.Fatalf("DetachFromThread(nil): %v", err)
	}
	// Cross-user scoping: another user's call must not detach this artifact.
	if err := s.DetachFromThread(ctx, "user_2", []string{kept.ID}); err != nil {
		t.Fatalf("DetachFromThread(user_2): %v", err)
	}
	if err := s.DetachFromThread(ctx, "user_1", []string{kept.ID}); err != nil {
		t.Fatalf("DetachFromThread: %v", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM threads WHERE user_id = 'user_1' AND id = 'thread_1'`); err != nil {
		t.Fatalf("delete thread: %v", err)
	}

	found, ok, err := s.Get(ctx, "user_1", kept.ID)
	if err != nil {
		t.Fatalf("Get kept: %v", err)
	}
	if !ok {
		t.Fatal("detached artifact was taken by the thread cascade")
	}
	if found.ThreadID != "" {
		t.Errorf("kept.ThreadID = %q, want empty", found.ThreadID)
	}
	if _, ok, err := s.Get(ctx, "user_1", swept.ID); err != nil || ok {
		t.Fatalf("still-attached artifact ok=%v err=%v, want gone with the thread", ok, err)
	}
}
