package artifact

import (
	"testing"
	"time"
)

// List joins threads, which has its own id and created_at: the keyset and the
// ORDER BY must name the artifacts alias on every column, or the boundary is
// ambiguous SQL.
func TestArtifactKeysetAndOrderQualifyTheArtifactsAlias(t *testing.T) {
	cursor := EncodeArtifactCursor(Artifact{ID: "art_1", DisplayFilename: "a", SizeBytes: 3, CreatedAt: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)}, SortByModified)
	for _, tc := range []struct {
		sort      SortBy
		wantKey   string
		wantOrder string
	}{
		{SortByModified, "(a.created_at, a.id) < (?, ?)", "a.created_at DESC, a.id DESC"},
		{SortByName, "(a.display_filename COLLATE NOCASE, a.id) < (?, ?)", "a.display_filename COLLATE NOCASE DESC, a.id DESC"},
		{SortBySize, "(a.size_bytes, a.id) < (?, ?)", "a.size_bytes DESC, a.id DESC"},
	} {
		c := cursor
		if tc.sort == SortBySize {
			c = EncodeArtifactCursor(Artifact{ID: "art_1", SizeBytes: 3}, SortBySize)
		}
		clause, _, err := artifactKeysetClause(tc.sort, SortDesc, c)
		if err != nil {
			t.Fatalf("%s: %v", tc.sort, err)
		}
		if clause.expr != tc.wantKey {
			t.Fatalf("%s keyset = %q, want %q", tc.sort, clause.expr, tc.wantKey)
		}
		if got := listOrderBy(tc.sort, SortDesc); got != tc.wantOrder {
			t.Fatalf("%s order = %q, want %q", tc.sort, got, tc.wantOrder)
		}
	}
}

func TestSQLiteTimeRendersBoundsInUTC(t *testing.T) {
	at := time.Date(2026, 10, 1, 2, 0, 0, 500_000_000, time.FixedZone("CEST", 2*3600))
	if got := sqliteTime(at); got != "2026-10-01 00:00:00" {
		t.Fatalf("sqliteTime = %q, want the UTC second in created_at's layout", got)
	}
}

func TestEffectiveArtifactLimit(t *testing.T) {
	if got := EffectiveArtifactLimit(0); got != 100 {
		t.Fatalf("EffectiveArtifactLimit(0) = %d, want the 100-row default", got)
	}
	if got := EffectiveArtifactLimit(5000); got != maxArtifactLimit {
		t.Fatalf("EffectiveArtifactLimit(5000) = %d, want the %d maximum", got, maxArtifactLimit)
	}
	if got := EffectiveArtifactLimit(25); got != 25 {
		t.Fatalf("EffectiveArtifactLimit(25) = %d, want 25", got)
	}
}
