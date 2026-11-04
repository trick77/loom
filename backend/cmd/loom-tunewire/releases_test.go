package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trick77/toolkit/applemusic"
)

func TestArtistNames(t *testing.T) {
	for in, want := range map[string]string{
		"Band":                  "Band",
		"A, B & C":              "A|B|C",
		"Main feat. Guest":      "Main|Guest",
		"Main ft. Guest & More": "Main|Guest|More",
		"  ":                    "",
	} {
		if got := strings.Join(artistNames(in), "|"); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}

func TestRankArtists_listeningFirstThenTheLibrary(t *testing.T) {
	lib := []applemusic.Artist{
		{ID: "r.1", Name: "Alpha", CatalogID: "1"},
		{ID: "r.2", Name: "Beta"},
		{ID: "r.3", Name: "Gamma", CatalogID: "3"},
		{ID: "r.4", Name: "gamma"},                 // same name, kept once
		{ID: "r.5", Name: "Delta", CatalogID: "3"}, // same catalog artist as Gamma
	}
	recent := []applemusic.Item{
		{Kind: "song", Name: "s1", Artist: "Gamma"},
		{Kind: "song", Name: "s2", Artist: "Gamma"},
		{Kind: "song", Name: "s3", Artist: "Beta & Nobody"}, // Beta by its part
		{Kind: "song", Name: "s4", Artist: "Zeta", ArtistID: "z.9"},
		{Kind: "song", Name: "s5", Artist: "Unknown"}, // no id: cannot be checked
		{Kind: "song", Name: "s6", Artist: ""},
	}
	heavy := []applemusic.Item{
		{Kind: "album", Name: "LP", Artist: "Beta", ArtistID: "b.2"},
		{Kind: "playlist", Name: "Hits", Artist: "Alpha"}, // a curator, not a band
	}
	top, fromListening := rankArtists(lib, signal{recent, weightPlayed}, signal{heavy, weightHeavy})
	var got []string
	for _, c := range top {
		got = append(got, fmt.Sprintf("%s:%s:%d", c.name, c.key("u"), c.score))
	}
	if want := "Beta:b.2:4 Gamma:3:2 Zeta:z.9:1 Alpha:1:0"; strings.Join(got, " ") != want {
		t.Errorf("ranked %v, want %s", got, want)
	}
	if fromListening != 3 {
		t.Errorf("fromListening %d", fromListening)
	}

	var many []applemusic.Artist
	for i := range maxReleaseArtists + 10 {
		many = append(many, applemusic.Artist{ID: fmt.Sprintf("r.%d", i), Name: fmt.Sprintf("Band %d", i)})
	}
	top, fromListening = rankArtists(many)
	// A library-only artist is keyed by its user: library ids are per library.
	if len(top) != maxReleaseArtists || fromListening != 0 || top[0].key("u") != "l:u:r.0" || top[0].key("v") == top[0].key("u") {
		t.Errorf("%d picked, %d from listening, first %+v", len(top), fromListening, top[0])
	}
}

func TestReleasedWithin(t *testing.T) {
	for released, want := range map[string]bool{
		"2026-10-02": true,  // 30 days back, the first day in
		"2026-10-01": false, // one day out
		"2026-12-24": true,  // a pre-release
		"2026-10":    false, // a month cannot tell
		"2026":       false,
		"":           false,
	} {
		if ok := releasedWithin(released, now.Add(5*time.Hour), 30); ok != want {
			t.Errorf("%q: %v", released, ok)
		}
	}
}

func TestReleaseCache_expiresAndStaysBounded(t *testing.T) {
	var c releaseCache
	c.put("a", cachedRelease{found: true, at: now})
	if _, ok := c.get("a", now.Add(releaseTTL-time.Second)); !ok {
		t.Error("fresh entry missed")
	}
	if _, ok := c.get("a", now.Add(releaseTTL)); ok {
		t.Error("expired entry served")
	}
	// Full of fresh entries: the oldest goes.
	c = releaseCache{}
	for i := range maxCachedReleases {
		c.put(fmt.Sprint(i), cachedRelease{at: now.Add(time.Duration(i) * time.Second)})
	}
	c.put("new", cachedRelease{at: now.Add(2 * time.Hour)})
	if _, ok := c.m["0"]; ok || len(c.m) != maxCachedReleases {
		t.Errorf("oldest kept, %d entries", len(c.m))
	}
	// A key already there is replaced in place.
	c.put("new", cachedRelease{at: now.Add(2 * time.Hour)})
	if len(c.m) != maxCachedReleases {
		t.Errorf("%d entries", len(c.m))
	}
	// Full with expired entries: they all go.
	c.put("later", cachedRelease{at: now.Add(releaseTTL + time.Hour)})
	if len(c.m) != 2 {
		t.Errorf("%d entries after expiry", len(c.m))
	}
}

// releasesFixture: Beta and Zeta listened to, Alpha, Eta and Theta in the
// library only.
func releasesFixture(api *fakeAPI) {
	api.libArtists = []applemusic.Artist{
		{ID: "r.1", Name: "Alpha", CatalogID: "1"},
		{ID: "r.2", Name: "Beta", CatalogID: "2"},
		{ID: "r.5", Name: "Eta"},   // catalog id only from the relationship call
		{ID: "r.6", Name: "Theta"}, // no catalog artist at all
	}
	api.catalogIDs = map[string]string{"r.5": "5"}
	api.recent = []applemusic.Item{{Kind: "song", Name: "s", Artist: "Beta"}, {Kind: "song", Name: "z", Artist: "Zeta", ArtistID: "9"}}
	api.latest = map[string]applemusic.Item{
		"2": {Kind: "album", Name: "Fresh", Artist: "Beta", Released: "2026-10-20", URL: "https://music.apple.com/ch/album/f", Artwork: "https://img/f.jpg"},
		"9": {Kind: "album", Name: "Soon", Artist: "Zeta", Released: "2026-11-20", URL: "https://music.apple.com/ch/album/s"},
		"5": {Kind: "album", Name: "Old", Artist: "Eta", Released: "2025-01-01", URL: "https://music.apple.com/ch/album/o"},
	}
}

func TestNewReleasesTool(t *testing.T) {
	svc, api, h := connected(t)
	svc.now = func() time.Time { return now }
	releasesFixture(api)
	api.libTruncated = true
	text, sc := callTool(t, h, "/mcp/library", "new_releases", `{}`)
	for _, want := range []string{
		"New releases from the user's artists on Apple Music (ch storefront), last 30 days.",
		"Artists picked: 5 artists, 2 by recent listening, 3 more artists from the library in library order (only its first 4 artists were read).",
		"[1] Soon — Zeta · out 2026-11-20 (pre-release)\n[2] Fresh — Beta · released 2026-10-20\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Old") || strings.Contains(text, "Partial") || strings.Contains(text, "could not be checked") {
		t.Errorf("text: %s", text)
	}
	if s := sources(sc); len(s) != 2 || s[1]["image"] != "https://img/f.jpg" {
		t.Errorf("sources %v", s)
	}
	if api.latestStorefront != "ch" || api.called("catalogID") != 2 || api.called("latest") != 4 {
		t.Errorf("storefront %q, %d catalog id calls, %d latest calls", api.latestStorefront, api.called("catalogID"), api.called("latest"))
	}

	// Asked again: the releases and the library come from memory.
	text, _ = callTool(t, h, "/mcp/library", "new_releases", `{"days":1000}`)
	if !strings.Contains(text, "last 120 days") || !strings.Contains(text, "Fresh") || strings.Contains(text, "Old") {
		t.Errorf("120 days: %s", text)
	}
	if api.called("latest") != 4 || api.called("catalogID") != 2 || api.called("artists") != 1 {
		t.Errorf("cache missed: %d latest, %d catalog id, %d library calls", api.called("latest"), api.called("catalogID"), api.called("artists"))
	}
	// Past the library list's time, it is read again; the releases are not.
	svc.now = func() time.Time { return now.Add(libraryArtistsTTL) }
	callTool(t, h, "/mcp/library", "new_releases", `{"days":5}`)
	if api.called("artists") != 2 || api.called("latest") != 4 {
		t.Errorf("%d library calls, %d latest", api.called("artists"), api.called("latest"))
	}
	// A pre-release shows whatever days says; past the cache's time Zeta's is
	// read again and gone.
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{"days":5}`); !strings.Contains(text, "Soon") || strings.Contains(text, "Fresh") {
		t.Errorf("5 days: %s", text)
	}
	delete(api.latest, "9")
	svc.now = func() time.Time { return now.Add(releaseTTL) }
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{"days":5}`); !strings.Contains(text, "None of the 5 artists checked released anything in the last 5 days.") {
		t.Errorf("none: %s", text)
	}
	if api.called("latest") != 8 {
		t.Errorf("expired releases not read again: %d", api.called("latest"))
	}

	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{"artist":"x"}`); !strings.HasPrefix(text, "ERROR: new_releases takes only days") {
		t.Errorf("bad args: %s", text)
	}
}

func TestNewReleasesTool_noArtists(t *testing.T) {
	_, _, h := connected(t)
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.Contains(text, "no favourite artists to check") {
		t.Errorf("nothing: %s", text)
	}
}

func TestNewReleasesTool_errors(t *testing.T) {
	svc, api, h := connected(t)
	svc.now = func() time.Time { return now }
	releasesFixture(api)

	// One artist failing is counted, not fatal.
	api.latestErr = map[string]error{"9": errors.New("apple returned 500")}
	text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`)
	if !strings.Contains(text, "Fresh") || !strings.Contains(text, "1 artist could not be checked: Apple answered with an error.") {
		t.Errorf("one failed: %s", text)
	}
	// The failure was not cached: the next call asks again.
	api.latestErr = nil
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.Contains(text, "Soon") {
		t.Errorf("retry: %s", text)
	}

	svc.releases = releaseCache{}
	api.latestErr = map[string]error{"2": applemusic.ErrDevTokenRefused}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.Contains(text, "The user's own sign-in is not the problem") {
		t.Errorf("dev refused: %s", text)
	}
	api.latestErr = nil

	// A list that fails narrows the artists; a refused token ends the call.
	svc.artists = artistsCache{}
	api.listenErr = map[string]error{"heavy": errors.New("400 bad limit")}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); strings.HasPrefix(text, "ERROR") || !strings.Contains(text, "Fresh") {
		t.Errorf("soft: %s", text)
	}
	svc.artists = artistsCache{}
	api.listenErr = map[string]error{"recent": applemusic.ErrUserTokenExpired}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.HasPrefix(text, "ERROR: Apple Music sign-in expired") {
		t.Errorf("expired: %s", text)
	}
	// Nothing to go by because every list failed: an error, not "no artists".
	svc.artists = artistsCache{}
	api.listenErr = map[string]error{"recent": errors.New("down"), "heavy": errors.New("down"), "added": errors.New("down"), "artists": errors.New("down")}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.HasPrefix(text, "ERROR: Apple Music is unavailable right now: down") {
		t.Errorf("all down: %s", text)
	}
	svc.releases = releaseCache{}
	api.listenErr = map[string]error{"catalogID": errors.New("down")}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.Contains(text, "2 artists could not be checked") {
		t.Errorf("mapping down (Eta, Theta): %s", text)
	}

	if _, err := svc.ClearToken(testUser); err != nil {
		t.Fatal(err)
	}
	if text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`); !strings.HasPrefix(text, "ERROR: Apple Music is not connected") {
		t.Errorf("not connected: %s", text)
	}
}

// At most releaseWorkers lookups run at once.
func TestNewReleases_boundedConcurrency(t *testing.T) {
	svc, api, h := connected(t)
	svc.now = func() time.Time { return now }
	api.latest = map[string]applemusic.Item{}
	for i := range 30 {
		id := fmt.Sprint(100 + i)
		api.libArtists = append(api.libArtists, applemusic.Artist{ID: "r." + id, Name: "Band " + id, CatalogID: id})
	}
	api.latestHook = func(context.Context, string) { time.Sleep(5 * time.Millisecond) }
	callTool(t, h, "/mcp/library", "new_releases", `{}`)
	if n := api.maxFlight.Load(); n > releaseWorkers {
		t.Errorf("max in flight %d", n)
	}
	if api.called("latest") != 30 {
		t.Errorf("%d lookups", api.called("latest"))
	}
}

// Out of time: what was checked is shown and the list says it is partial;
// the next call finishes from memory.
func TestNewReleases_partialWhenTimeRunsOut(t *testing.T) {
	svc, api, h := connected(t)
	svc.now = func() time.Time { return now }
	svc.releaseBudget = 100 * time.Millisecond
	releasesFixture(api)
	api.latestHook = func(ctx context.Context, id string) {
		if id == "9" {
			<-ctx.Done()
		}
	}
	text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`)
	if !strings.Contains(text, "Fresh") || strings.Contains(text, "Soon") ||
		!strings.Contains(text, "Partial list: only 4 of the 5 artists could be checked in time.") ||
		strings.Index(text, "Partial list") > strings.Index(text, "Fresh") {
		t.Errorf("partial: %s", text)
	}
	api.latestHook = nil
	before := api.called("latest")
	text, _ = callTool(t, h, "/mcp/library", "new_releases", `{}`)
	if !strings.Contains(text, "Soon") || strings.Contains(text, "Partial") || api.called("latest") != before+1 {
		t.Errorf("second call: %s (%d new lookups)", text, api.called("latest")-before)
	}
}

// The main artist is credited by its catalog id: "X feat. A" credits X even
// when only A is in the library, and "X" and "X & B" are one candidate.
func TestRankArtists_mainArtistByCatalogID(t *testing.T) {
	lib := []applemusic.Artist{{ID: "r.1", Name: "A", CatalogID: "1"}}
	played := []applemusic.Item{
		{Kind: "song", Name: "s1", Artist: "X feat. A", ArtistID: "7"},
		{Kind: "song", Name: "s2", Artist: "X & B", ArtistID: "7"},
		{Kind: "song", Name: "s3", Artist: "X", ArtistID: "7"},
	}
	top, fromListening := rankArtists(lib, signal{played, weightPlayed})
	if len(top) != 2 || top[0].catalogID != "7" || top[0].score != 3 || top[1].name != "A" || top[1].score != 1 || fromListening != 2 {
		t.Errorf("top = %+v, fromListening %d", top, fromListening)
	}
}

// Two artists with one name but different catalog ids stay two: the
// listened one is checked, the library one keeps its own id.
func TestRankArtists_sameNameOtherArtist(t *testing.T) {
	lib := []applemusic.Artist{{ID: "r.1", Name: "Phoenix", CatalogID: "111"}}
	played := []applemusic.Item{{Kind: "song", Name: "s", Artist: "Phoenix", ArtistID: "222"}}
	top, _ := rankArtists(lib, signal{played, weightPlayed})
	ids := map[string]int{}
	for _, c := range top {
		ids[c.catalogID] = c.score
	}
	if len(top) != 2 || ids["222"] != 1 || ids["111"] != 0 {
		t.Errorf("top = %+v", top)
	}
}

// Cached answers are used even when the budget is gone before any lookup.
func TestLookupReleases_cachedAnswersSurviveASpentBudget(t *testing.T) {
	svc, _, _ := connected(t)
	svc.now = func() time.Time { return now }
	cands := []candidate{{name: "A", catalogID: "1"}, {name: "B", catalogID: "2"}}
	svc.releases.put("ch|1", cachedRelease{release: applemusic.Item{Name: "Cached"}, found: true, at: now})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outs := svc.lookupReleases(ctx, "ch", cands)
	if !outs[0].done || outs[0].release.Name != "Cached" || outs[1].done {
		t.Errorf("outs = %+v", outs)
	}
}

// A source that could not be read is named in the answer.
func TestNewReleases_saysWhichSourceIsMissing(t *testing.T) {
	svc, api, h := connected(t)
	svc.now = func() time.Time { return now }
	releasesFixture(api)
	api.listenErr = map[string]error{"artists": errors.New("apple returned 500")}
	text, _ := callTool(t, h, "/mcp/library", "new_releases", `{}`)
	if !strings.Contains(text, "Incomplete: Apple could not give the user's library artists right now") {
		t.Errorf("text: %s", text)
	}
}
