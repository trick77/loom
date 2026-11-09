package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/location"
)

// fakeLocationLLM reads a home from the first "Lives …" fact and a stay from
// the first "In … this week" fact, ending on stayUntil.
type fakeLocationLLM struct {
	mu        sync.Mutex
	calls     int
	today     string
	stayUntil string
	err       error
}

func (f *fakeLocationLLM) ResolveUserLocation(_ context.Context, facts []llm.FactInput, today string) (llm.UserLocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.today = today
	if f.err != nil {
		return llm.UserLocation{}, f.err
	}
	var out llm.UserLocation
	for _, fact := range facts {
		if place, ok := strings.CutPrefix(fact.Content, "Lives "); ok && out.Home == nil {
			place, _, _ = strings.Cut(place, ",")
			out.Home = &llm.PlaceRef{Place: place, FactID: fact.ID}
		}
		if rest, ok := strings.CutPrefix(fact.Content, "In "); ok && out.Current == nil {
			// A country the lookup does not find the place in: it retries
			// without.
			out.Current = &llm.PlaceRef{Place: strings.TrimSuffix(rest, " this week"), Country: "PT", Until: f.stayUntil, FactID: fact.ID}
		}
	}
	return out, nil
}

// geocoder answers the weather sidecar's lookup for a few towns and counts
// the calls.
type geocoder struct {
	mu      sync.Mutex
	queries []string
}

func (g *geocoder) call(_ context.Context, name string, args map[string]any) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if name != geocodeTool {
		return "", errors.New("unexpected tool " + name)
	}
	q, _ := args["query"].(string)
	if country, _ := args["country"].(string); country != "" {
		g.queries = append(g.queries, q+"|"+country)
		return `{"places":[]}`, nil
	}
	g.queries = append(g.queries, q)
	switch q {
	case "Zürich":
		return `{"places":[{"name":"Zürich","region":"Zürich","country":"Schweiz","latitude":47.37,"longitude":8.54,"timezone":"Europe/Zurich"}]}`, nil
	case "Lisbon":
		return `{"places":[{"name":"Lisbon","country":"Portugal","latitude":38.72,"longitude":-9.14,"timezone":"Europe/Lisbon"}]}`, nil
	case "Broken":
		return "not json", nil
	}
	return `{"places":[]}`, nil
}

type locationFixture struct {
	db     *sql.DB
	deps   Deps
	s      *server
	chat   *chat.Store
	places *location.Store
	llm    *fakeLocationLLM
	geo    *geocoder
}

func newLocationFixture(t *testing.T) *locationFixture {
	t.Helper()
	db := newUserDB(t)
	f := &locationFixture{db: db, chat: chat.NewStore(db), places: location.NewStore(db), llm: &fakeLocationLLM{stayUntil: "2026-11-15"}, geo: &geocoder{}}
	f.deps = Deps{
		Thread:      f.chat,
		Usage:       stubUsageStore{},
		LLM:         &fakeChatClient{},
		Users:       fakeUserStore{user: testUser, ok: true},
		MCP:         fakeMCPService{Available: map[string]bool{geocodeTool: true}, CallFunc: f.geo.call},
		Locations:   f.places,
		LocationLLM: f.llm,
	}
	f.s = newServer(f.deps)
	f.s.now = func() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) }
	return f
}

func (f *locationFixture) addFact(t *testing.T, content string) {
	t.Helper()
	if _, err := f.chat.AddUserFact(context.Background(), testUser.ID, "", chat.FactCategoryPersonal, content, true); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLocationFollowsTheFacts(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	user := testUser
	user.Timezone = "Europe/Zurich"

	// No facts: nothing to read, no model call.
	if err := f.s.resolveLocation(ctx, user); err != nil || f.llm.calls != 0 {
		t.Fatalf("no facts: calls=%d, %v", f.llm.calls, err)
	}
	f.addFact(t, "Lives Zürich")
	if err := f.s.resolveLocation(ctx, user); err != nil {
		t.Fatal(err)
	}
	if f.llm.today != "2026-11-11" {
		t.Fatalf("today = %q, want the user's date", f.llm.today)
	}
	here, err := f.s.userPlace(ctx, user)
	if err != nil || here == nil || here.Name != "Zürich" || here.Kind != location.KindHome || here.Timezone != "Europe/Zurich" {
		t.Fatalf("here = %+v, %v", here, err)
	}
	// Unchanged facts: no model call, no lookup.
	if err := f.s.resolveLocation(ctx, user); err != nil || f.llm.calls != 1 || len(f.geo.queries) != 1 {
		t.Fatalf("unchanged: calls=%d lookups=%v, %v", f.llm.calls, f.geo.queries, err)
	}

	// A stay: read anew, home reused without a lookup, the stay holds
	// through its last day, then home again.
	f.addFact(t, "In Lisbon this week")
	if err := f.s.resolveLocation(ctx, user); err != nil {
		t.Fatal(err)
	}
	if f.llm.calls != 2 || strings.Join(f.geo.queries, ",") != "Zürich,Lisbon|PT,Lisbon" {
		t.Fatalf("stay: calls=%d lookups=%v", f.llm.calls, f.geo.queries)
	}
	if here, _ := f.s.userPlace(ctx, user); here == nil || here.Name != "Lisbon" || here.Until != "2026-11-15" {
		t.Fatalf("during the stay: %+v", here)
	}
	f.s.now = func() time.Time { return time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) }
	if here, _ := f.s.userPlace(ctx, user); here == nil || here.Name != "Zürich" {
		t.Fatalf("after the stay: %+v", here)
	}
}

func TestResolveLocationOffOrFailing(t *testing.T) {
	ctx := context.Background()
	f := newLocationFixture(t)
	f.addFact(t, "Lives Zürich")
	// Without the weather lookup places are off: no model call.
	f.s.mcp = fakeMCPService{}
	if err := f.s.resolveLocation(ctx, testUser); err != nil || f.llm.calls != 0 {
		t.Fatalf("off: calls=%d, %v", f.llm.calls, err)
	}
	f.s.mcp = fakeMCPService{Available: map[string]bool{geocodeTool: true}, CallFunc: f.geo.call}
	// A failed read keeps nothing, so the next sweep tries again.
	f.llm.err = errors.New("gate down")
	if err := f.s.resolveLocation(ctx, testUser); err == nil {
		t.Fatal("a failed read gave no error")
	}
	if _, _, hash, _ := f.places.Places(ctx, testUser.ID); hash != "" {
		t.Fatalf("a failed read was recorded: %q", hash)
	}
	f.llm.err = nil
	// A place the lookup does not know is no place; a broken lookup an error.
	f.addFact(t, "In Atlantis this week")
	if err := f.s.resolveLocation(ctx, testUser); err != nil {
		t.Fatal(err)
	}
	if _, current, _, _ := f.places.Places(ctx, testUser.ID); current != nil {
		t.Fatalf("an unknown place was kept: %+v", current)
	}
	// Facts seen in the same second come in no set order: one stay per user.
	broken := newLocationFixture(t)
	broken.addFact(t, "In Broken this week")
	if err := broken.s.resolveLocation(ctx, testUser); err == nil {
		t.Fatal("an unreadable lookup gave no error")
	}
}

func TestSetLocation(t *testing.T) {
	f := newLocationFixture(t)
	handler := newAuthenticatedServer(t, f.deps)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/me/location", body))
		return rec
	}

	ctx := context.Background()
	// A stay derived before survives setting the home.
	stay := &location.Place{Name: "Lisbon", Latitude: 38.7, Longitude: -9.1, Since: "2026-11-01", Until: "2099-01-01"}
	if err := f.places.Save(ctx, testUser.ID, "old", nil, stay); err != nil {
		t.Fatal(err)
	}
	rec := post(`{"place":"  Zürich "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}
	var got struct{ Place location.Place }
	// The answer is the home the user named, not the stay that holds today.
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Place.Name != "Zürich" || got.Place.Kind != location.KindHome {
		t.Fatalf("place = %s, %v", rec.Body, err)
	}
	// It is a fact in the user's memory, the one source of their place, and
	// kept as the home without a model call.
	facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID)
	if len(facts) != 1 || facts[0].Content != "Lives Zürich, Schweiz" || !facts[0].Durable {
		t.Fatalf("facts = %+v", facts)
	}
	home, current, hash, _ := f.places.Places(ctx, testUser.ID)
	if home == nil || home.FactID != facts[0].ID || current == nil || current.Name != "Lisbon" || hash != location.HashFacts(facts) || f.llm.calls != 0 {
		t.Fatalf("kept: home %+v, current %+v, hash %q, calls %d", home, current, hash, f.llm.calls)
	}
	// The same place again adds nothing; a move rewrites the home fact.
	if rec := post(`{"place":"Zürich"}`); rec.Code != http.StatusOK {
		t.Fatalf("again = %d", rec.Code)
	}
	if rec := post(`{"place":"Lisbon"}`); rec.Code != http.StatusOK {
		t.Fatalf("move = %d", rec.Code)
	}
	facts, _ = f.chat.ListActiveUserFacts(ctx, testUser.ID)
	if len(facts) != 1 || facts[0].Content != "Lives Lisbon, Portugal" {
		t.Fatalf("after a move facts = %+v", facts)
	}

	if rec := post(`{"place":"Atlantis"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("unknown place = %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"place":" "}`, `{"place":"` + strings.Repeat("x", 101) + `"}`, `nope`} {
		if rec := post(body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, rec.Code)
		}
	}

	off := f.deps
	off.MCP = fakeMCPService{}
	rec = httptest.NewRecorder()
	newAuthenticatedServer(t, off).ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/me/location", `{"place":"Zürich"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("off = %d", rec.Code)
	}
}

func TestGeocodeLanguageAndToday(t *testing.T) {
	for lang, want := range map[string]string{"de": "de", "de-CH": "de", "": "en", "German": "en"} {
		if got := geocodeLanguage(auth.User{ResponseLanguage: lang}); got != want {
			t.Errorf("geocodeLanguage(%q) = %q, want %q", lang, got, want)
		}
	}
	late := time.Date(2026, 11, 11, 23, 30, 0, 0, time.UTC)
	if got := userToday(auth.User{Timezone: "Europe/Zurich"}, late); got != "2026-11-12" {
		t.Errorf("Zurich after 23:30 UTC = %s", got)
	}
	if got := userToday(auth.User{}, late); got != "2026-11-11" {
		t.Errorf("no zone = %s", got)
	}
}
