package artifact

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/trick77/slopr/internal/store"
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

func TestStoreListsArtifactsByUserWithFiltersSearchAndSort(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user'),
       ('user_2', 'subject-user_2', 'user_2', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts'),
       ('thread_2', 'user_2', 'Other Artifacts')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
VALUES ('art_pdf', 'user_1', 'thread_1', 'Quarterly Board.pdf', 'files/outputs/board.pdf', 'application/pdf', 1400000, 'assistant_generated', '2026-06-09 10:00:00'),
       ('art_png', 'user_1', 'thread_1', 'Robot Concept.png', 'files/outputs/robot.png', 'image/png', 842000, 'assistant_generated', '2026-06-10 09:00:00'),
       ('art_csv', 'user_1', 'thread_1', '100% literal.csv', 'files/outputs/literal.csv', 'text/csv', 12000, 'uploaded', '2026-06-08 08:00:00'),
       ('art_other', 'user_2', 'thread_2', 'Robot Other.png', 'files/outputs/other.png', 'image/png', 10, 'uploaded', '2026-06-10 10:00:00')`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)
	all, err := s.List(ctx, "user_1", ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	assertArtifactIDs(t, all, []string{"art_png", "art_pdf", "art_csv"})

	images, err := s.List(ctx, "user_1", ListOptions{Type: ListTypeImages})
	if err != nil {
		t.Fatalf("List(images) error = %v", err)
	}
	assertArtifactIDs(t, images, []string{"art_png"})

	files, err := s.List(ctx, "user_1", ListOptions{Type: ListTypeFiles, Sort: SortByName, Order: SortAsc})
	if err != nil {
		t.Fatalf("List(files) error = %v", err)
	}
	assertArtifactIDs(t, files, []string{"art_csv", "art_pdf"})

	matches, err := s.List(ctx, "user_1", ListOptions{Search: "100%"})
	if err != nil {
		t.Fatalf("List(search) error = %v", err)
	}
	assertArtifactIDs(t, matches, []string{"art_csv"})

	bySize, err := s.List(ctx, "user_1", ListOptions{Sort: SortBySize, Order: SortAsc})
	if err != nil {
		t.Fatalf("List(size asc) error = %v", err)
	}
	assertArtifactIDs(t, bySize, []string{"art_csv", "art_png", "art_pdf"})
}

func assertArtifactIDs(t *testing.T, artifacts []Artifact, want []string) {
	t.Helper()
	got := make([]string, 0, len(artifacts))
	for _, item := range artifacts {
		got = append(got, item.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact ids = %#v, want %#v", got, want)
	}
}
