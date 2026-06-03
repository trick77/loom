package artifact

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/spark/internal/store"
)

func TestStoreCreatesAndFindsArtifactByUser(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)
	created, err := s.Create(context.Background(), CreateInput{
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		VolumeRelPath:   "files/outputs/report.pdf",
		MIMEType:        "application/pdf",
		SizeBytes:       12,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, ok, err := s.Get(context.Background(), "user_1", created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false")
	}
	if found.DisplayFilename != "report.pdf" || found.DownloadURL == "" {
		t.Fatalf("found = %#v", found)
	}

	if _, ok, err := s.Get(context.Background(), "user_2", created.ID); err != nil || ok {
		t.Fatalf("cross-user Get() ok=%v err=%v, want false nil", ok, err)
	}
}
