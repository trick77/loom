package location_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range []string{"u1", "u2"} {
		if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`, id, "sub-"+id, id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestStoreSavesAndReplacesPlaces(t *testing.T) {
	ctx := context.Background()
	st := location.NewStore(openDB(t))
	if home, current, hash, err := st.Places(ctx, "u1"); err != nil || home != nil || current != nil || hash != "" {
		t.Fatalf("never derived: %v %v %q %v", home, current, hash, err)
	}
	home := &location.Place{Name: "Zürich", Country: "Schweiz", Latitude: 47.37, Longitude: 8.54, Timezone: "Europe/Zurich", FactID: "f1"}
	stay := &location.Place{Name: "Lisbon", Latitude: 38.7, Longitude: -9.1, Since: "2026-11-10", Until: "2026-11-15", FactID: "f2", Query: "lisbon|PT"}
	if err := st.Save(ctx, "u1", "h1", home, stay); err != nil {
		t.Fatal(err)
	}
	gotHome, gotCurrent, hash, err := st.Places(ctx, "u1")
	if err != nil || hash != "h1" || gotHome == nil || gotHome.Name != "Zürich" || gotHome.Kind != location.KindHome || gotHome.Until != "" ||
		gotCurrent == nil || gotCurrent.Since != "2026-11-10" || gotCurrent.Until != "2026-11-15" || gotCurrent.FactID != "f2" || gotCurrent.Query != "lisbon|PT" {
		t.Fatalf("places = %+v, %+v, %q, %v", gotHome, gotCurrent, hash, err)
	}
	// Another user sees none of it.
	if h, c, hash, err := st.Places(ctx, "u2"); err != nil || h != nil || c != nil || hash != "" {
		t.Fatalf("another user: %v %v %q %v", h, c, hash, err)
	}
	// Saving again replaces both.
	if err := st.Save(ctx, "u1", "h2", nil, nil); err != nil {
		t.Fatal(err)
	}
	if h, c, hash, err := st.Places(ctx, "u1"); err != nil || h != nil || c != nil || hash != "h2" {
		t.Fatalf("after clearing: %v %v %q %v", h, c, hash, err)
	}
}

func TestHere(t *testing.T) {
	home := &location.Place{Kind: location.KindHome, Name: "Zürich"}
	stay := &location.Place{Kind: location.KindCurrent, Name: "Lisbon", Since: "2026-11-09", Until: "2026-11-15"}
	for _, tc := range []struct {
		home, current *location.Place
		today, want   string
	}{
		{home, stay, "2026-11-08", "Zürich"},
		{home, stay, "2026-11-09", "Lisbon"},
		{home, stay, "2026-11-15", "Lisbon"},
		{home, stay, "2026-11-16", "Zürich"},
		{nil, stay, "2026-11-16", ""},
		{home, nil, "2026-11-09", "Zürich"},
	} {
		got := location.Here(tc.home, tc.current, tc.today)
		if (got == nil && tc.want != "") || (got != nil && got.Name != tc.want) {
			t.Errorf("Here on %s = %+v, want %q", tc.today, got, tc.want)
		}
	}
}

func TestHashFacts(t *testing.T) {
	day := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	facts := []chat.UserFact{{ID: "f1", Content: "Lives Zurich", LastSeen: day}}
	base := location.HashFacts(facts)
	if location.HashFacts(facts) != base {
		t.Fatal("the hash is not stable")
	}
	later := []chat.UserFact{{ID: "f1", Content: "Lives Zurich", LastSeen: day.Add(2 * time.Hour)}}
	if location.HashFacts(later) != base {
		t.Fatal("seen again the same day changed the hash")
	}
	for name, changed := range map[string][]chat.UserFact{
		"edited":   {{ID: "f1", Content: "Lives Bern", LastSeen: day}},
		"next day": {{ID: "f1", Content: "Lives Zurich", LastSeen: day.AddDate(0, 0, 1)}},
		"added":    {facts[0], {ID: "f2", Content: "In Lisbon this week", LastSeen: day}},
		"gone":     nil,
	} {
		if location.HashFacts(changed) == base {
			t.Errorf("%s: the hash did not change", name)
		}
	}
}

func TestStoreReportsFailures(t *testing.T) {
	db := openDB(t)
	st := location.NewStore(db)
	_ = db.Close()
	if _, _, _, err := st.Places(context.Background(), "u1"); err == nil {
		t.Fatal("Places on a closed db: no error")
	}
	if err := st.Save(context.Background(), "u1", "h", nil, nil); err == nil {
		t.Fatal("Save on a closed db: no error")
	}
}
