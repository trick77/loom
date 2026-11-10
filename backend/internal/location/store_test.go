package location_test

import (
	"context"
	"database/sql"
	"errors"
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

// A derivation that read the places before someone saved them must not
// overwrite them.
func TestSaveIfLeavesNewerPlaces(t *testing.T) {
	ctx := context.Background()
	st := location.NewStore(openDB(t))
	userSet := &location.Place{Name: "Zürich", FactID: "f2"}
	if err := st.Save(ctx, "u1", "after", userSet, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveIf(ctx, "u1", "before", "derived", nil, nil); !errors.Is(err, location.ErrStale) {
		t.Fatalf("stale SaveIf = %v, want ErrStale", err)
	}
	if home, _, hash, _ := st.Places(ctx, "u1"); home == nil || home.Name != "Zürich" || hash != "after" {
		t.Fatalf("places overwritten: %+v %q", home, hash)
	}
	if err := st.SaveIf(ctx, "u1", "after", "derived", nil, nil); err != nil {
		t.Fatalf("current SaveIf = %v", err)
	}
	// Never derived: the read hash is "".
	if err := st.SaveIf(ctx, "u2", "", "first", nil, nil); err != nil {
		t.Fatalf("first SaveIf = %v", err)
	}
}

// The located town is the user's own, apart from the places their memory
// names: a derivation saving places leaves it, and only the user drops it.
func TestStoreKeepsLocatedApart(t *testing.T) {
	ctx := context.Background()
	st := location.NewStore(openDB(t))
	if got, err := st.Located(ctx, "u1"); err != nil || got != nil {
		t.Fatalf("never located: %+v %v", got, err)
	}
	bern := location.Located{
		Place:  location.Place{Name: "Bern", Region: "Bern", Country: "Schweiz", Latitude: 46.95, Longitude: 7.45, Timezone: "Europe/Zurich"},
		Credit: location.Credit{Text: "© OpenStreetMap", URL: "https://osm.org/copyright"},
	}
	if err := st.SaveLocated(ctx, "u1", bern); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, "u1", "h1", &location.Place{Name: "Luzern", FactID: "f1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, "u1", "h2", nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := st.Located(ctx, "u1")
	if err != nil || got == nil || got.Name != "Bern" || got.Kind != location.KindLocated || got.Timezone != "Europe/Zurich" ||
		got.Latitude != 46.95 || got.Credit != bern.Credit {
		t.Fatalf("located after derivations = %+v, %v", got, err)
	}
	if other, err := st.Located(ctx, "u2"); err != nil || other != nil {
		t.Fatalf("another user: %+v %v", other, err)
	}
	// Locating again replaces it.
	bern.Name = "Thun"
	if err := st.SaveLocated(ctx, "u1", bern); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Located(ctx, "u1"); got == nil || got.Name != "Thun" {
		t.Fatalf("located again = %+v", got)
	}
	if err := st.ClearLocated(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Located(ctx, "u1"); err != nil || got != nil {
		t.Fatalf("after clearing: %+v %v", got, err)
	}
	// Nothing to clear is no error.
	if err := st.ClearLocated(ctx, "u1"); err != nil {
		t.Fatal(err)
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
	if _, err := st.Located(context.Background(), "u1"); err == nil {
		t.Fatal("Located on a closed db: no error")
	}
	if err := st.SaveLocated(context.Background(), "u1", location.Located{}); err == nil {
		t.Fatal("SaveLocated on a closed db: no error")
	}
	if err := st.ClearLocated(context.Background(), "u1"); err == nil {
		t.Fatal("ClearLocated on a closed db: no error")
	}
}
