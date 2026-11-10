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
// kilometre before it leaves loom. Saving that name with the point keeps the
// town it named, not the search's first of that name, and so does every
// derivation after.
func TestLocateAndSaveNearThePoint(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	f.deps.MCP = fakeMCPService{Available: map[string]bool{weather.GeocodeTool: true, weather.ReverseGeocodeTool: true}, CallFunc: f.geo.call}
	f.s.mcp = f.deps.MCP
	handler := newAuthenticatedServer(t, f.deps)
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, path, body))
		return rec
	}

	rec := post("/api/me/location:locate", `{"latitude":47.47712,"longitude":8.30851}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("locate = %d %s", rec.Code, rec.Body)
	}
	var located struct {
		Place       location.Place
		Attribution weather.Attribution
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &located); err != nil || located.Place.Name != "Baden" || located.Place.Region != "Aargau" ||
		!strings.Contains(located.Attribution.Text, "OpenStreetMap") {
		t.Fatalf("located = %s, %v", rec.Body, err)
	}
	if lat, lon := f.geo.points[0]["latitude"], f.geo.points[0]["longitude"]; lat != 47.48 || lon != 8.31 {
		t.Fatalf("point sent = %v, %v; want it rounded", lat, lon)
	}
	// Locating saves nothing: the user saves the name.
	if facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID); len(facts) != 0 {
		t.Fatalf("locating wrote facts: %+v", facts)
	}

	rec = post("/api/me/location", `{"place":"Baden","near":{"latitude":47.47,"longitude":8.31}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body)
	}
	home, _, _, _ := f.places.Places(ctx, testUser.ID)
	if home == nil || home.Name != "Baden" || home.Timezone != "Europe/Zurich" {
		t.Fatalf("home = %+v, want the Swiss Baden", home)
	}
	// Another fact makes the model read the facts again; the home it names
	// ("Baden", "CH") is looked up anew and must stay the town saved.
	f.addFact(t, "Likes cycling")
	if err := f.s.resolveLocation(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if home, _, _, _ := f.places.Places(ctx, testUser.ID); home == nil || home.Timezone != "Europe/Zurich" || home.Latitude != 47.47 {
		t.Fatalf("after a derivation home = %+v, want the Swiss Baden kept", home)
	}

	// A name whose town the search by name misses among its namesakes is
	// still the town the point named.
	if rec := post("/api/me/location", `{"place":"Neustadt","near":{"latitude":49.35,"longitude":8.14}}`); rec.Code != http.StatusOK {
		t.Fatalf("namesake save = %d %s", rec.Code, rec.Body)
	}
	if home, _, _, _ := f.places.Places(ctx, testUser.ID); home == nil || home.Latitude != 49.35 {
		t.Fatalf("namesake home = %+v, want the Neustadt located", home)
	}

	// Without the point the name is the search's first, as before.
	if rec := post("/api/me/location", `{"place":"Lisbon"}`); rec.Code != http.StatusOK {
		t.Fatalf("move = %d", rec.Code)
	}
	if rec := post("/api/me/location", `{"place":"Baden"}`); rec.Code != http.StatusOK {
		t.Fatalf("plain save = %d", rec.Code)
	}
	if home, _, _, _ := f.places.Places(ctx, testUser.ID); home == nil || home.Name != "Baden-Baden" {
		t.Fatalf("plain home = %+v", home)
	}

	// The sea: no town. A point off the globe: refused.
	if rec := post("/api/me/location:locate", `{"latitude":38,"longitude":-12}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("the sea = %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"latitude":91,"longitude":0}`, `{"latitude":1}`, `nope`} {
		if rec := post("/api/me/location:locate", body); rec.Code != http.StatusBadRequest {
			t.Errorf("locate %s = %d", body, rec.Code)
		}
	}
	if rec := post("/api/me/location", `{"place":"Baden","near":{"latitude":1,"longitude":181}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("save near off the globe = %d", rec.Code)
	}

	// Locating saves nothing, however often.
	if facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID); len(facts) != 2 {
		t.Fatalf("facts = %+v", facts)
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

	// A server without the reverse lookup: no locating.
	rec = httptest.NewRecorder()
	newAuthenticatedServer(t, newLocationFixture(t).deps).ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/me/location:locate", `{"latitude":47.4,"longitude":8.3}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no reverse lookup = %d", rec.Code)
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
