package artifact

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/trick77/slopr/internal/store"
)

func TestStoreCreatesThreadlessUploadWithSource(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)
	// A global upload has no thread.
	created, err := s.Create(ctx, CreateInput{
		UserID:          "u",
		DisplayFilename: "g.pdf",
		VolumeRelPath:   "files/g.pdf",
		MIMEType:        "application/pdf",
		SizeBytes:       10,
		Source:          "user_uploaded",
	})
	if err != nil {
		t.Fatalf("Create() thread-less error = %v", err)
	}
	if created.ThreadID != "" {
		t.Errorf("ThreadID = %q, want empty for global upload", created.ThreadID)
	}

	found, ok, err := s.Get(ctx, "u", created.ID)
	if err != nil || !ok {
		t.Fatalf("Get() ok=%v err=%v", ok, err)
	}
	if found.Source != "user_uploaded" {
		t.Errorf("Source = %q, want user_uploaded", found.Source)
	}
	if found.ThreadID != "" {
		t.Errorf("found ThreadID = %q, want empty", found.ThreadID)
	}
}
