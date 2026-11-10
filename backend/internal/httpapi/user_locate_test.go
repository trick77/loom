package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/weather"
)

// "Use my location": the browser's point names the town, rounded to about a
// kilometre before it leaves loom. The town is kept apart from the memory and
// holds over the places it names, whatever a later derivation reads, until
// the user drops it. It is never a fact.
func TestLocateHoldsOverTheMemory(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	f.deps.MCP = fakeMCPService{Available: map[string]bool{weather.GeocodeTool: true, weather.ReverseGeocodeTool: true}, CallFunc: f.geo.call}
	f.s.mcp = f.deps.MCP
	handler := newAuthenticatedServer(t, f.deps)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authenticatedRequest(method, path, body))
		return rec
	}
	// The memory says Zürich.
	f.addFact(t, "Lives Zürich")
	if err := f.s.resolveLocation(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.s.userPlace(ctx, testUser); p == nil || p.Name != "Zürich" {
		t.Fatalf("from memory = %+v", p)
	}

	rec := do(http.MethodPost, "/api/me/location:locate", `{"latitude":47.47712,"longitude":8.30851}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("locate = %d %s", rec.Code, rec.Body)
	}
	var located struct {
		Place       location.Place
		Attribution weather.Attribution
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &located); err != nil || located.Place.Name != "Baden" || located.Place.Region != "Aargau" ||
		located.Place.Kind != location.KindLocated || !strings.Contains(located.Attribution.Text, "OpenStreetMap") {
		t.Fatalf("located = %s, %v", rec.Body, err)
	}
	if lat, lon := f.geo.points[0]["latitude"], f.geo.points[0]["longitude"]; lat != 47.48 || lon != 8.31 {
		t.Fatalf("point sent = %v, %v; want it rounded", lat, lon)
	}
	// Kept with its credit, over the memory's home, here and in turns.
	kept, _ := f.places.Located(ctx, testUser.ID)
	if kept == nil || kept.Name != "Baden" || kept.Timezone != "Europe/Zurich" || kept.Credit.URL != "https://www.openstreetmap.org/copyright" {
		t.Fatalf("kept = %+v", kept)
	}
	if p, _ := f.s.userPlace(ctx, testUser); p == nil || p.Name != "Baden" || p.Kind != location.KindLocated {
		t.Fatalf("here = %+v, want the located town", p)
	}
	// No fact: the memory still says Zürich.
	if facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID); len(facts) != 1 || facts[0].Content != "Lives Zürich" {
		t.Fatalf("facts = %+v", facts)
	}
	// A derivation reading changed facts leaves the located town.
	f.addFact(t, "In Lisbon this week")
	if err := f.s.resolveLocation(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.s.userPlace(ctx, testUser); p == nil || p.Name != "Baden" {
		t.Fatalf("after a derivation here = %+v", p)
	}

	// Dropped: the memory holds again.
	if rec := do(http.MethodDelete, "/api/me/location:locate", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("drop = %d %s", rec.Code, rec.Body)
	}
	if p, _ := f.s.userPlace(ctx, testUser); p == nil || p.Name != "Lisbon" {
		t.Fatalf("after dropping here = %+v, want the stay from memory", p)
	}

	// The sea: no town, nothing kept. A point off the globe: refused.
	if rec := do(http.MethodPost, "/api/me/location:locate", `{"latitude":38,"longitude":-12}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("the sea = %d %s", rec.Code, rec.Body)
	}
	if kept, _ := f.places.Located(ctx, testUser.ID); kept != nil {
		t.Fatalf("the sea kept %+v", kept)
	}
	for _, body := range []string{`{"latitude":91,"longitude":0}`, `{"latitude":1}`, `nope`} {
		if rec := do(http.MethodPost, "/api/me/location:locate", body); rec.Code != http.StatusBadRequest {
			t.Errorf("locate %s = %d", body, rec.Code)
		}
	}

	// A busy map provider: a retry is worth it, said so.
	busy := f.deps
	busy.MCP = fakeMCPService{Available: map[string]bool{weather.GeocodeTool: true, weather.ReverseGeocodeTool: true},
		CallFunc: func(context.Context, string, map[string]any) (string, error) {
			return "", errors.New("reverse lookup failed: map provider busy, try again shortly")
		}}
	rec = httptest.NewRecorder()
	newAuthenticatedServer(t, busy).ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/me/location:locate", `{"latitude":47.4,"longitude":8.3}`))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "try again") {
		t.Fatalf("busy = %d %s", rec.Code, rec.Body)
	}

	// A server without the reverse lookup: no locating. Dropping still works:
	// a town located before must not stay stuck.
	plain := newLocationFixture(t)
	if err := plain.places.SaveLocated(ctx, testUser.ID, location.Located{Place: location.Place{Name: "Baden"}}); err != nil {
		t.Fatal(err)
	}
	plainHandler := newAuthenticatedServer(t, plain.deps)
	rec = httptest.NewRecorder()
	plainHandler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/me/location:locate", `{"latitude":47.4,"longitude":8.3}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no reverse lookup = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	plainHandler.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/me/location:locate", ""))
	if kept, _ := plain.places.Located(ctx, testUser.ID); rec.Code != http.StatusNoContent || kept != nil {
		t.Fatalf("drop without the reverse lookup = %d, kept %+v", rec.Code, kept)
	}
}

// A turn's model reads where the user is from the personal context: the
// located town is named there, over the place the facts name, or one chat
// answers for two places.
func TestUserContextNamesTheLocatedTown(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	f.addFact(t, "Lives Zürich")
	if out := f.s.userContextForUser(ctx, testUser.ID); strings.Contains(out, "current location") {
		t.Fatalf("no located town, yet: %s", out)
	}
	baden := location.Located{Place: location.Place{Name: "Baden", Region: "Aargau", Country: "Schweiz"}}
	if err := f.places.SaveLocated(ctx, testUser.ID, baden); err != nil {
		t.Fatal(err)
	}
	out := f.s.userContextForUser(ctx, testUser.ID)
	if !strings.Contains(out, "Lives Zürich") || !strings.Contains(out, "Baden, Aargau, Schweiz") ||
		strings.Index(out, "Baden, Aargau") < strings.Index(out, "Lives Zürich") {
		t.Fatalf("context = %s, want the located town after the facts", out)
	}
}

// A located town that cannot be read leaves the memory's place: the page
// keeps its weather, a turn its outdoor windows.
func TestPlaceHereFallsBackToTheMemory(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	f.addFact(t, "Lives Zürich")
	if err := f.s.resolveLocation(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DROP TABLE user_located_places`); err != nil {
		t.Fatal(err)
	}
	p, credit, err := f.s.placeHere(ctx, testUser)
	if err != nil || p == nil || p.Name != "Zürich" || credit != nil {
		t.Fatalf("here = %+v, %+v, %v; want the memory's home", p, credit, err)
	}
	// A turn's context keeps the facts.
	if out := f.s.userContextForUser(ctx, testUser.ID); !strings.Contains(out, "Lives Zürich") || strings.Contains(out, "current location") {
		t.Fatalf("context = %s", out)
	}
}

// Only the same name looked up again keeps the town it was; a new name is a
// move, looked up plainly, or a namesake near the old home would win.
func TestSameTownName(t *testing.T) {
	kept := &location.Place{Name: "Baden AG", Query: "baden ag|"}
	for name, want := range map[string]bool{
		"Baden AG": true, "baden": true, "Baden AG Schweiz": true,
		"Wil": false, "Badenweiler": false, "": false,
	} {
		if got := sameTownName(kept, name); got != want {
			t.Errorf("%q = %v, want %v", name, got, want)
		}
	}
}
