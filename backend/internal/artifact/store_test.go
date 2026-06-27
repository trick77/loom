package artifact

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/trick77/loom/internal/store"
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

func TestStoreListPaginatesWithCursor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title) VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	// Mixed casing on names exercises the COLLATE NOCASE keyset boundary.
	if _, err := db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
VALUES ('art_a', 'user_1', 'thread_1', 'alpha.txt', 'f/a.txt', 'text/plain', 1, 'uploaded', '2026-06-10 09:00:00'),
       ('art_b', 'user_1', 'thread_1', 'Bravo.txt', 'f/b.txt', 'text/plain', 2, 'uploaded', '2026-06-10 09:00:01'),
       ('art_c', 'user_1', 'thread_1', 'charlie.txt', 'f/c.txt', 'text/plain', 3, 'uploaded', '2026-06-10 09:00:02'),
       ('art_d', 'user_1', 'thread_1', 'Delta.txt', 'f/d.txt', 'text/plain', 4, 'uploaded', '2026-06-10 09:00:03'),
       ('art_e', 'user_1', 'thread_1', 'echo.txt', 'f/e.txt', 'text/plain', 5, 'uploaded', '2026-06-10 09:00:04')`); err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)

	// modified DESC: newest first.
	wantModified := []string{"art_e", "art_d", "art_c", "art_b", "art_a"}
	assertArtifactIDs(t, collectPages(t, s, ListOptions{Sort: SortByModified, Order: SortDesc, Limit: 2}), wantModified)

	// name ASC with case-insensitive collation: alpha, Bravo, charlie, Delta, echo.
	wantName := []string{"art_a", "art_b", "art_c", "art_d", "art_e"}
	assertArtifactIDs(t, collectPages(t, s, ListOptions{Sort: SortByName, Order: SortAsc, Limit: 2}), wantName)

	// size DESC.
	wantSize := []string{"art_e", "art_d", "art_c", "art_b", "art_a"}
	assertArtifactIDs(t, collectPages(t, s, ListOptions{Sort: SortBySize, Order: SortDesc, Limit: 2}), wantSize)
}

// collectPages walks every page via the returned next cursor and concatenates
// the results, asserting no page exceeds the limit.
func collectPages(t *testing.T, s *Store, opts ListOptions) []Artifact {
	t.Helper()
	ctx := context.Background()
	var all []Artifact
	limit := EffectiveArtifactLimit(opts.Limit)
	for {
		page, err := s.List(ctx, "user_1", opts)
		if err != nil {
			t.Fatalf("List(cursor=%q) error = %v", opts.Cursor, err)
		}
		all = append(all, page...)
		if len(page) < limit {
			break
		}
		opts.Cursor = EncodeArtifactCursor(page[len(page)-1], opts.Sort)
	}
	return all
}

func TestStoreSoftDeleteHidesFromListAndGet(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)

	created, err := s.Create(ctx, CreateInput{
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

	if err := s.Delete(ctx, "user_1", created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Soft-deleted: gone from list and from the user-facing Get.
	all, err := s.List(ctx, "user_1", ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	assertArtifactIDs(t, all, []string{})
	if _, ok, err := s.Get(ctx, "user_1", created.ID); err != nil || ok {
		t.Fatalf("Get() after delete ok=%v err=%v, want false nil", ok, err)
	}

	// The row survives for the tombstone overlay: GetMany still returns it, flagged.
	many, err := s.GetMany(ctx, "user_1", []string{created.ID})
	if err != nil {
		t.Fatalf("GetMany() error = %v", err)
	}
	got, ok := many[created.ID]
	if !ok {
		t.Fatalf("GetMany() missing soft-deleted artifact")
	}
	if !got.Deleted {
		t.Fatalf("GetMany() Deleted = false, want true")
	}
	if got.DisplayFilename != "report.pdf" {
		t.Fatalf("GetMany() DisplayFilename = %q, want report.pdf", got.DisplayFilename)
	}

	// Ensure deleted_at was actually stamped (not merely filtered by chance).
	var deletedAt sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT deleted_at FROM artifacts WHERE id = ?`, created.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at error = %v", err)
	}
	if !deletedAt.Valid {
		t.Fatalf("deleted_at is NULL, want a timestamp")
	}
}

func TestStoreRenameUpdatesDisplayFilename(t *testing.T) {
	ctx := context.Background()
	_, s := newArtifactTestStore(t)

	created, err := s.Create(ctx, CreateInput{
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "draft.pdf",
		VolumeRelPath:   "files/outputs/draft.pdf",
		MIMEType:        "application/pdf",
		SizeBytes:       12,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := s.Rename(ctx, "user_1", created.ID, "final.pdf"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	found, ok, err := s.Get(ctx, "user_1", created.ID)
	if err != nil || !ok {
		t.Fatalf("Get() after rename ok=%v err=%v", ok, err)
	}
	if found.DisplayFilename != "final.pdf" {
		t.Fatalf("DisplayFilename = %q, want final.pdf", found.DisplayFilename)
	}

	// A foreign user cannot rename.
	if err := s.Rename(ctx, "user_2", created.ID, "hijacked.pdf"); err != nil {
		t.Fatalf("Rename(other user) error = %v", err)
	}
	found, _, _ = s.Get(ctx, "user_1", created.ID)
	if found.DisplayFilename != "final.pdf" {
		t.Fatalf("cross-user rename changed name to %q", found.DisplayFilename)
	}
}

// newArtifactTestStore opens a fresh migrated DB with one user and thread, and
// returns the handle plus a Store. Mirrors the inline setup used across the suite.
func newArtifactTestStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user'),
       ('user_2', 'subject-user_2', 'user_2', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	return db, NewStore(db)
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

func TestStoreThumbnailRelPathRoundTripAndDerivedURL(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)

	// A raster image created with an eager thumbnail relpath round-trips it and
	// exposes the derived thumbnail URL.
	img, err := s.Create(ctx, CreateInput{
		UserID:           "user_1",
		ThreadID:         "thread_1",
		DisplayFilename:  "robot.png",
		VolumeRelPath:    "files/outputs/robot.png",
		MIMEType:         "image/png",
		SizeBytes:        10,
		ThumbnailRelPath: ThumbnailRelPath("files/outputs/robot.png"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantURL := "/api/artifacts/" + img.ID + "/thumbnail"
	wantThumbRel := ThumbnailRelPath("files/outputs/robot.png")
	if img.ThumbnailRelPath != wantThumbRel || img.ThumbnailURL != wantURL {
		t.Fatalf("create result relpath=%q url=%q, want stored relpath and %q", img.ThumbnailRelPath, img.ThumbnailURL, wantURL)
	}
	got, ok, err := s.Get(ctx, "user_1", img.ID)
	if err != nil || !ok {
		t.Fatalf("Get() ok=%v err=%v", ok, err)
	}
	if got.ThumbnailRelPath != wantThumbRel || got.ThumbnailURL != wantURL {
		t.Fatalf("get result relpath=%q url=%q, want round-trip and %q", got.ThumbnailRelPath, got.ThumbnailURL, wantURL)
	}

	// A raster image created WITHOUT a thumbnail still advertises the URL (lazy
	// backfill fulfils it on first view) but has no stored relpath yet.
	noThumb, err := s.Create(ctx, CreateInput{
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "photo.jpg",
		VolumeRelPath:   "files/outputs/photo.jpg",
		MIMEType:        "image/jpeg",
		SizeBytes:       10,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if noThumb.ThumbnailRelPath != "" {
		t.Fatalf("ThumbnailRelPath = %q, want empty before backfill", noThumb.ThumbnailRelPath)
	}
	if noThumb.ThumbnailURL == "" {
		t.Fatal("raster image without a thumbnail should still advertise a thumbnail URL")
	}

	// SetThumbnailRelPath persists onto the existing row (the lazy-backfill path).
	if err := s.SetThumbnailRelPath(ctx, "user_1", noThumb.ID, ThumbnailRelPath("files/outputs/photo.jpg")); err != nil {
		t.Fatalf("SetThumbnailRelPath() error = %v", err)
	}
	after, _, err := s.Get(ctx, "user_1", noThumb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ThumbnailRelPath != ThumbnailRelPath("files/outputs/photo.jpg") {
		t.Fatalf("after backfill relpath = %q, want persisted", after.ThumbnailRelPath)
	}

	// A non-raster artifact (SVG) carries no thumbnail URL.
	svg, err := s.Create(ctx, CreateInput{
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "diagram.svg",
		VolumeRelPath:   "files/outputs/diagram.svg",
		MIMEType:        "image/svg+xml; charset=utf-8",
		SizeBytes:       10,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if svg.ThumbnailURL != "" {
		t.Fatalf("SVG ThumbnailURL = %q, want empty (served as its own preview)", svg.ThumbnailURL)
	}

	// List also surfaces the thumbnail URL for raster images.
	items, err := s.List(ctx, "user_1", ListOptions{Type: ListTypeImages})
	if err != nil {
		t.Fatal(err)
	}
	var sawImg bool
	for _, it := range items {
		if it.ID == img.ID {
			sawImg = true
			if it.ThumbnailURL != wantURL {
				t.Fatalf("List ThumbnailURL = %q, want %q", it.ThumbnailURL, wantURL)
			}
		}
	}
	if !sawImg {
		t.Fatal("List did not return the raster image artifact")
	}
}
