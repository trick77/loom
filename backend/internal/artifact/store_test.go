package artifact

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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
	assertListItemIDs(t, all, []string{"art_png", "art_pdf", "art_csv"})

	images, err := s.List(ctx, "user_1", ListOptions{Type: ListTypeImages})
	if err != nil {
		t.Fatalf("List(images) error = %v", err)
	}
	assertListItemIDs(t, images, []string{"art_png"})

	files, err := s.List(ctx, "user_1", ListOptions{Type: ListTypeFiles, Sort: SortByName, Order: SortAsc})
	if err != nil {
		t.Fatalf("List(files) error = %v", err)
	}
	assertListItemIDs(t, files, []string{"art_csv", "art_pdf"})

	matches, err := s.List(ctx, "user_1", ListOptions{Search: "100%"})
	if err != nil {
		t.Fatalf("List(search) error = %v", err)
	}
	assertListItemIDs(t, matches, []string{"art_csv"})

	bySize, err := s.List(ctx, "user_1", ListOptions{Sort: SortBySize, Order: SortAsc})
	if err != nil {
		t.Fatalf("List(size asc) error = %v", err)
	}
	assertListItemIDs(t, bySize, []string{"art_csv", "art_png", "art_pdf"})
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
	assertListItemIDs(t, collectPages(t, s, ListOptions{Sort: SortByModified, Order: SortDesc, Limit: 2}), wantModified)

	// name ASC with case-insensitive collation: alpha, Bravo, charlie, Delta, echo.
	wantName := []string{"art_a", "art_b", "art_c", "art_d", "art_e"}
	assertListItemIDs(t, collectPages(t, s, ListOptions{Sort: SortByName, Order: SortAsc, Limit: 2}), wantName)

	// size DESC.
	wantSize := []string{"art_e", "art_d", "art_c", "art_b", "art_a"}
	assertListItemIDs(t, collectPages(t, s, ListOptions{Sort: SortBySize, Order: SortDesc, Limit: 2}), wantSize)

	// Every sort pages the same with a thread-title search and a period bound
	// active: the keyset stays aligned with the joined ORDER BY.
	bounded := ListOptions{
		Search: "artifacts",
		Since:  time.Date(2026, 6, 10, 9, 0, 1, 0, time.UTC),
		Until:  time.Date(2026, 6, 10, 9, 0, 4, 0, time.UTC),
		Limit:  2,
	}
	for _, tc := range []struct {
		sort  SortBy
		order SortOrder
		want  []string
	}{
		{SortByModified, SortDesc, []string{"art_d", "art_c", "art_b"}},
		{SortByModified, SortAsc, []string{"art_b", "art_c", "art_d"}},
		{SortByName, SortDesc, []string{"art_d", "art_c", "art_b"}},
		{SortByName, SortAsc, []string{"art_b", "art_c", "art_d"}},
		{SortBySize, SortDesc, []string{"art_d", "art_c", "art_b"}},
		{SortBySize, SortAsc, []string{"art_b", "art_c", "art_d"}},
	} {
		opts := bounded
		opts.Sort, opts.Order = tc.sort, tc.order
		items := collectPages(t, s, opts)
		assertListItemIDs(t, items, tc.want)
		for _, item := range items {
			if item.ThreadTitle != "Artifacts" {
				t.Fatalf("%s %s: ThreadTitle = %q, want Artifacts", tc.sort, tc.order, item.ThreadTitle)
			}
		}
	}
}

// collectPages walks every page via the returned next cursor and concatenates
// the results, asserting no page exceeds the limit.
func collectPages(t *testing.T, s *Store, opts ListOptions) []ListItem {
	t.Helper()
	ctx := context.Background()
	var all []ListItem
	limit := EffectiveArtifactLimit(opts.Limit)
	for {
		page, err := s.List(ctx, "user_1", opts)
		if err != nil {
			t.Fatalf("List(cursor=%q) error = %v", opts.Cursor, err)
		}
		if len(page) > limit {
			t.Fatalf("page of %d exceeds limit %d", len(page), limit)
		}
		all = append(all, page...)
		if len(page) < limit {
			break
		}
		opts.Cursor = EncodeArtifactCursor(page[len(page)-1].Artifact, opts.Sort)
	}
	return all
}

// The library's period bound is [Since, Until): a row stamped exactly at Since
// is in, one stamped exactly at Until is out. Bounds in another zone are
// compared as UTC, the zone created_at is stored in.
func TestStoreListBoundsByPeriod(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
VALUES ('before', 'user_1', 'thread_1', 'a.txt', 'f/a', 'text/plain', 1, 'uploaded', '2026-09-30 23:59:59'),
       ('at_since', 'user_1', 'thread_1', 'b.txt', 'f/b', 'text/plain', 1, 'uploaded', '2026-10-01 00:00:00'),
       ('inside', 'user_1', 'thread_1', 'c.txt', 'f/c', 'text/plain', 1, 'uploaded', '2026-10-15 12:00:00'),
       ('last', 'user_1', 'thread_1', 'd.txt', 'f/d', 'text/plain', 1, 'uploaded', '2026-10-31 23:59:59'),
       ('at_until', 'user_1', 'thread_1', 'e.txt', 'f/e', 'text/plain', 1, 'uploaded', '2026-11-01 00:00:00'),
       ('after', 'user_1', 'thread_1', 'f.txt', 'f/f', 'text/plain', 1, 'uploaded', '2026-11-02 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	zurich := time.FixedZone("CET", 3600)
	since := time.Date(2026, 10, 1, 1, 0, 0, 0, zurich) // 2026-10-01 00:00:00 UTC
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		opts ListOptions
		want []string
	}{
		"both":       {ListOptions{Since: since, Until: until}, []string{"last", "inside", "at_since"}},
		"since only": {ListOptions{Since: since}, []string{"after", "at_until", "last", "inside", "at_since"}},
		"until only": {ListOptions{Until: until}, []string{"last", "inside", "at_since", "before"}},
	} {
		items, err := s.List(ctx, "user_1", tc.opts)
		if err != nil {
			t.Fatalf("%s: List() error = %v", name, err)
		}
		assertListItemIDs(t, items, tc.want)
	}
}

// Search matches the artifact's filename or its thread's title, only ever
// within the caller's own artifacts.
func TestStoreListSearchesThreadTitle(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title) VALUES ('thread_plan', 'user_1', 'Quarterly planning'),
                                                ('thread_foreign', 'user_2', 'Quarterly planning');
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
VALUES ('by_title', 'user_1', 'thread_plan', 'chart.png', 'f/chart', 'image/png', 1, 'assistant_generated', '2026-10-03 00:00:00'),
       ('by_name', 'user_1', NULL, 'planning-notes.txt', 'f/notes', 'text/plain', 1, 'user_uploaded', '2026-10-02 00:00:00'),
       ('no_match', 'user_1', 'thread_1', 'robot.png', 'f/robot', 'image/png', 1, 'assistant_generated', '2026-10-01 00:00:00'),
       ('foreign', 'user_2', 'thread_foreign', 'secret.png', 'f/secret', 'image/png', 1, 'assistant_generated', '2026-10-04 00:00:00')`); err != nil {
		t.Fatal(err)
	}

	items, err := s.List(ctx, "user_1", ListOptions{Search: "PLANNING"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	assertListItemIDs(t, items, []string{"by_title", "by_name"})
	if items[0].ThreadTitle != "Quarterly planning" {
		t.Fatalf("ThreadTitle = %q, want the thread's title", items[0].ThreadTitle)
	}
	if items[1].ThreadTitle != "" || items[1].ThreadID != "" {
		t.Fatalf("thread-less artifact: ThreadTitle=%q ThreadID=%q, want both empty", items[1].ThreadTitle, items[1].ThreadID)
	}

	// The other user's identically-titled thread never leaks across.
	foreign, err := s.List(ctx, "user_2", ListOptions{Search: "planning"})
	if err != nil {
		t.Fatalf("List(user_2) error = %v", err)
	}
	assertListItemIDs(t, foreign, []string{"foreign"})

	// LIKE wildcards in the term stay literal against the title too.
	none, err := s.List(ctx, "user_1", ListOptions{Search: "Quarterly_planning"})
	if err != nil {
		t.Fatalf("List(wildcard) error = %v", err)
	}
	assertListItemIDs(t, none, []string{})
}

func TestStoreSummaryCountsMonthsAndOlder(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title) VALUES ('thread_2', 'user_2', 'Other');
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, deleted_at)
VALUES ('nov_png', 'user_1', 'thread_1', 'a.png', 'f/a', 'image/png', 1, 'uploaded', '2026-11-03 08:00:00', NULL),
       ('nov_pdf', 'user_1', 'thread_1', 'b.pdf', 'f/b', 'application/pdf', 1, 'uploaded', '2026-11-20 08:00:00', NULL),
       ('oct_end', 'user_1', 'thread_1', 'c.png', 'f/c', 'image/png', 1, 'uploaded', '2026-10-31 23:59:59', NULL),
       ('oct_start', 'user_1', NULL, 'd.csv', 'f/d', 'text/csv', 1, 'uploaded', '2026-10-01 00:00:00', NULL),
       ('sep_gone', 'user_1', 'thread_1', 'e.png', 'f/e', 'image/png', 1, 'uploaded', '2026-09-10 08:00:00', '2026-09-11 00:00:00'),
       ('sep_last', 'user_1', 'thread_1', 'f.txt', 'f/f', 'text/plain', 1, 'uploaded', '2026-09-30 23:59:59', NULL),
       ('aug', 'user_1', 'thread_1', 'g.png', 'f/g', 'image/png', 1, 'uploaded', '2026-08-15 08:00:00', NULL),
       ('jul_foreign', 'user_2', 'thread_2', 'h.png', 'f/h', 'image/png', 1, 'uploaded', '2026-07-01 08:00:00', NULL)`); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 1, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600)) // 2026-10-01 00:00:00 UTC

	for name, tc := range map[string]struct {
		opts SummaryOptions
		want Summary
	}{
		"all": {SummaryOptions{Type: ListTypeAll, Since: since}, Summary{OlderCount: 2, Months: []MonthCount{
			{Month: "2026-11", Count: 2}, {Month: "2026-10", Count: 2}, {Month: "2026-09", Count: 1}, {Month: "2026-08", Count: 1},
		}}},
		"images": {SummaryOptions{Type: ListTypeImages, Since: since}, Summary{OlderCount: 1, Months: []MonthCount{
			{Month: "2026-11", Count: 1}, {Month: "2026-10", Count: 1}, {Month: "2026-08", Count: 1},
		}}},
		"files": {SummaryOptions{Type: ListTypeFiles, Since: since}, Summary{OlderCount: 1, Months: []MonthCount{
			{Month: "2026-11", Count: 1}, {Month: "2026-10", Count: 1}, {Month: "2026-09", Count: 1},
		}}},
		"no since": {SummaryOptions{}, Summary{OlderCount: 0, Months: []MonthCount{
			{Month: "2026-11", Count: 2}, {Month: "2026-10", Count: 2}, {Month: "2026-09", Count: 1}, {Month: "2026-08", Count: 1},
		}}},
	} {
		got, err := s.Summary(ctx, "user_1", tc.opts)
		if err != nil {
			t.Fatalf("%s: Summary() error = %v", name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: Summary() = %+v, want %+v", name, got, tc.want)
		}
	}

	// A closed database surfaces as an error from both library reads.
	defer func() {
		_ = db.Close()
		if _, err := s.Summary(ctx, "user_1", SummaryOptions{}); err == nil {
			t.Fatal("Summary() on a closed DB returned no error")
		}
		if _, err := s.List(ctx, "user_1", ListOptions{}); err == nil {
			t.Fatal("List() on a closed DB returned no error")
		}
	}()

	// A user with no artifacts gets an empty, non-nil month list.
	empty, err := s.Summary(ctx, "user_3", SummaryOptions{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Months == nil || len(empty.Months) != 0 || empty.OlderCount != 0 {
		t.Fatalf("empty Summary() = %#v, want zero count and an empty month list", empty)
	}
}

// The client lists a month by its local bounds, so the summary buckets in the
// client's zone: just past local midnight on Jan 1 is January there, whatever
// UTC says.
func TestStoreSummaryBucketsMonthsInTheGivenZone(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO threads (id, user_id, title) VALUES ('thread_2', 'user_2', 'Other');
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, deleted_at)
VALUES ('east_jan', 'user_1', 'thread_1', 'a.png', 'f/a', 'image/png', 1, 'uploaded', '2025-12-31 23:30:00', NULL),
       ('west_jan', 'user_1', 'thread_1', 'b.pdf', 'f/b', 'application/pdf', 1, 'uploaded', '2026-01-01 05:30:00', NULL),
       ('west_dec', 'user_1', 'thread_1', 'c.png', 'f/c', 'image/png', 1, 'uploaded', '2026-01-01 00:30:00', NULL),
       ('gone', 'user_1', 'thread_1', 'd.png', 'f/d', 'image/png', 1, 'uploaded', '2026-01-01 00:45:00', '2026-01-02 00:00:00'),
       ('foreign', 'user_2', 'thread_2', 'e.png', 'f/e', 'image/png', 1, 'uploaded', '2026-01-01 00:45:00', NULL)`); err != nil {
		t.Fatal(err)
	}
	load := func(name string) *time.Location {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		return loc
	}

	for name, tc := range map[string]struct {
		opts SummaryOptions
		want []MonthCount
	}{
		// UTC+1: east_jan is 00:30 on Jan 1, west_dec 01:30, west_jan 06:30.
		"east": {SummaryOptions{Location: load("Europe/Zurich")}, []MonthCount{{Month: "2026-01", Count: 3}}},
		// UTC-5: west_jan is 00:30 on Jan 1; the other two are still December.
		"west": {SummaryOptions{Location: load("America/New_York")}, []MonthCount{
			{Month: "2026-01", Count: 1}, {Month: "2025-12", Count: 2},
		}},
		// No zone is UTC.
		"utc":         {SummaryOptions{}, []MonthCount{{Month: "2026-01", Count: 2}, {Month: "2025-12", Count: 1}}},
		"east images": {SummaryOptions{Type: ListTypeImages, Location: load("Europe/Zurich")}, []MonthCount{{Month: "2026-01", Count: 2}}},
	} {
		got, err := s.Summary(ctx, "user_1", tc.opts)
		if err != nil {
			t.Fatalf("%s: Summary() error = %v", name, err)
		}
		if !reflect.DeepEqual(got.Months, tc.want) {
			t.Fatalf("%s: Months = %+v, want %+v", name, got.Months, tc.want)
		}
	}

	// A created_at that is no time fails the summary rather than miscount.
	if _, err := db.ExecContext(ctx, `
INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
VALUES ('bad', 'user_1', 'thread_1', 'x.png', 'f/x', 'image/png', 1, 'uploaded', 'not a time')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Summary(ctx, "user_1", SummaryOptions{}); err == nil {
		t.Fatal("Summary() with an unparseable created_at returned no error")
	}
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
	assertListItemIDs(t, all, []string{})
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

func assertListItemIDs(t *testing.T, items []ListItem, want []string) {
	t.Helper()
	artifacts := make([]Artifact, 0, len(items))
	for _, item := range items {
		artifacts = append(artifacts, item.Artifact)
	}
	assertArtifactIDs(t, artifacts, want)
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
	// Versioned, so a browser never reuses an older, smaller thumbnail it
	// cached as immutable.
	wantURL := "/api/artifacts/" + img.ID + "/thumbnail?v=2"
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

// Artifacts created in the same second (one run_python job saving two charts)
// list in creation order: created_at has one-second precision, so it needs a
// tie-breaker, or view_image's "newest" picks either.
// A project's delete takes its threads along, and with them artifacts whose
// own project_id is not the project's: a composer upload carries none, an
// artifact made before its thread moved in carries the old one. Their files
// must be on the cleanup list too.
func TestStoreListForProjectIncludesArtifactsOfItsThreads(t *testing.T) {
	ctx := context.Background()
	db, s := newArtifactTestStore(t)
	if _, err := db.ExecContext(ctx, `
INSERT INTO projects (id, user_id, name) VALUES ('proj_a', 'user_1', 'A'), ('proj_b', 'user_1', 'B');
INSERT INTO threads (id, user_id, title, project_id) VALUES ('thread_a', 'user_1', 'In A', 'proj_a');
INSERT INTO threads (id, user_id, title, project_id) VALUES ('thread_other', 'user_2', 'Not mine', NULL)`); err != nil {
		t.Fatal(err)
	}
	projectA, projectB := "proj_a", "proj_b"
	for _, in := range []CreateInput{
		{UserID: "user_1", ThreadID: "thread_a", ProjectID: &projectA, DisplayFilename: "generated.png"},
		{UserID: "user_1", ThreadID: "thread_a", DisplayFilename: "composer-upload.pdf"},
		{UserID: "user_1", ThreadID: "thread_a", ProjectID: &projectB, DisplayFilename: "made-before-move.png"},
		{UserID: "user_1", ProjectID: &projectA, DisplayFilename: "project-upload.pdf"},
		{UserID: "user_1", ThreadID: "thread_1", DisplayFilename: "elsewhere.png"},
	} {
		in.VolumeRelPath, in.MIMEType, in.SizeBytes = "files/"+in.DisplayFilename, "application/octet-stream", 1
		if _, err := s.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.ListForProject(ctx, "user_1", "proj_a")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range items {
		got = append(got, item.DisplayFilename)
	}
	want := []string{"generated.png", "composer-upload.pdf", "made-before-move.png", "project-upload.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListForProject = %v, want %v", got, want)
	}
}

func TestStoreListForThreadKeepsCreationOrderWithinASecond(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role) VALUES ('user_1', 'subject-user_1', 'user_1', 'user');
INSERT INTO threads (id, user_id, title) VALUES ('thread_1', 'user_1', 'Charts')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	var want []string
	for _, name := range []string{"a.png", "b.png", "c.png", "d.png"} {
		created, err := s.Create(ctx, CreateInput{
			UserID: "user_1", ThreadID: "thread_1", DisplayFilename: name,
			VolumeRelPath: "files/outputs/" + name, MIMEType: "image/png", SizeBytes: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, created.DisplayFilename)
	}
	if _, err := db.ExecContext(ctx, `UPDATE artifacts SET created_at = '2026-11-10 12:00:00'`); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListForThread(ctx, "user_1", "thread_1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range items {
		got = append(got, item.DisplayFilename)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListForThread order = %v, want creation order %v", got, want)
	}
}
