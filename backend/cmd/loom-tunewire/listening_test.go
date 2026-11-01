package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
)

// connected is a test service with the token set; the sidecar's configured
// storefront is "us", the owner's own (from the token check) "ch".
func connected(t *testing.T) (*service, *fakeAPI, http.Handler) {
	t.Helper()
	svc, api, _ := newTestService(t)
	svc.storefront = "us"
	h := newServer(config{token: testToken}, svc, "test")
	if text, _ := callTool(t, h, "/mcp/library", "recently_played", `{}`); !strings.Contains(text, "ERROR: Apple Music is not connected") {
		t.Fatalf("not connected: %s", text)
	}
	if _, err := svc.SetToken(context.Background(), "good"); err != nil {
		t.Fatal(err)
	}
	return svc, api, h
}

func sources(sc map[string]any) []map[string]any {
	var out []map[string]any
	for _, s := range sc["sources"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

func TestRecentlyPlayedTool(t *testing.T) {
	_, api, h := connected(t)
	if text, _ := callTool(t, h, "/mcp/library", "recently_played", `{}`); text != "The user has no recently played tracks on Apple Music." {
		t.Errorf("empty: %s", text)
	}
	api.recent = []applemusic.Item{
		{Kind: "song", Name: "Song", Artist: "Band", Album: "LP", Released: "2026-10-01", Duration: 225 * time.Second, URL: "https://music.apple.com/ch/song/1", Artwork: "https://img/1.jpg"},
		{Kind: "song", Name: "Own Rip", Artist: "Me"},
		{Kind: "song", Name: "Other", Artist: "Band", URL: "https://music.apple.com/ch/song/2"},
	}
	text, sc := callTool(t, h, "/mcp/library", "recently_played", `{"kind":"recent"}`)
	if !strings.Contains(text, "newest first (3):\n[1] Song — Band · LP · 2026 · 3:45\n- Own Rip — Me\n[2] Other — Band\n") {
		t.Errorf("recent: %s", text)
	}
	if s := sources(sc); len(s) != 2 || s[0]["url"] != "https://music.apple.com/ch/song/1" || s[0]["image"] != "https://img/1.jpg" || s[1]["title"] != "Other — Band" {
		t.Errorf("sources: %v", s)
	}

	if text, _ := callTool(t, h, "/mcp/library", "recently_played", `{"kind":"heavy_rotation"}`); text != "Apple Music has no heavy rotation for the user yet." {
		t.Errorf("empty heavy: %s", text)
	}
	api.heavy = []applemusic.Item{
		{Kind: "album", Name: "LP", Artist: "Band", URL: "https://music.apple.com/ch/album/1"},
		{Kind: "playlist", Name: "Hits", Artist: "Apple Music", URL: "https://music.apple.com/ch/playlist/pl.1"},
	}
	if text, _ := callTool(t, h, "/mcp/library", "recently_played", `{"kind":"heavy_rotation"}`); !strings.Contains(text, "(heavy rotation, 2):\n[1] album: LP — Band\n[2] playlist: Hits — Apple Music\n") {
		t.Errorf("heavy: %s", text)
	}

	api.listenErr = map[string]error{"recent": applemusic.ErrUserTokenExpired}
	for args, want := range map[string]string{
		`{"kind":"top"}`:    "ERROR: kind must be recent or heavy_rotation",
		`{"limit":3}`:       "ERROR: recently_played takes only kind",
		`{"kind":"recent"}`: "ERROR: Apple Music sign-in expired",
	} {
		if text, _ := callTool(t, h, "/mcp/library", "recently_played", args); !strings.HasPrefix(text, want) {
			t.Errorf("%s: %s", args, text)
		}
	}
	api.listenErr = map[string]error{"heavy": applemusic.ErrDevTokenRefused}
	if text, _ := callTool(t, h, "/mcp/library", "recently_played", `{"kind":"heavy_rotation"}`); !strings.Contains(text, "The user's own sign-in is not the problem") {
		t.Errorf("dev refused: %s", text)
	}
}

// A refused token seen by a listening call shows in the settings page.
func TestListening_recordsTheTokenState(t *testing.T) {
	svc, api, h := connected(t)
	api.refuse = true
	callTool(t, h, "/mcp/library", "new_music", `{"section":"for_you"}`)
	if st := svc.status(); st.State != "expired" {
		t.Errorf("state %+v", st)
	}
}

func newMusicFixture(api *fakeAPI) {
	for i := range 12 {
		api.added = append(api.added, applemusic.Item{Kind: "album", Name: fmt.Sprintf("Added %d", i), Artist: "Band",
			Added: "2026-10-30T08:00:00Z", URL: fmt.Sprintf("https://music.apple.com/ch/album/a%d", i)})
	}
	var many []applemusic.Item
	for i := range 9 {
		many = append(many, applemusic.Item{Kind: "album", Name: fmt.Sprintf("Release %d", i), Artist: "Someone"})
	}
	api.recs = []applemusic.Recommendation{
		{Title: "Made for You", Reason: "Updated Fridays", Items: []applemusic.Item{
			{ID: "pl.pm-1", Kind: "playlist", Name: "New Music Mix", Artist: "Apple Music", URL: "https://music.apple.com/ch/playlist/pl.pm-1"},
		}},
		{Title: "New Releases", Items: many},
	}
	api.mix = map[string][]applemusic.Item{"pl.pm-1": {
		{Kind: "song", Name: "Brand New", Artist: "Band", URL: "https://music.apple.com/ch/song/n1"},
	}}
}

func TestNewMusicTool(t *testing.T) {
	_, api, h := connected(t)
	newMusicFixture(api)

	text, sc := callTool(t, h, "/mcp/library", "new_music", `{}`)
	for _, want := range []string{
		"New on Apple Music for the user (ch storefront).",
		"[1] album: Added 0 — Band · added 2026-10-30\n",
		"[10] album: Added 9 — Band",
		"… and 2 more; ask for section recently_added to see them.",
		"Made for You — Updated Fridays:\n[11] playlist: New Music Mix — Apple Music\n",
		"New Releases (8 of 9 listed):\n- album: Release 0 — Someone\n",
		"Tracks in the user's New Music Mix:\n[12] Brand New — Band\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Release 8") {
		t.Errorf("past maxRecItems: %s", text)
	}
	if s := sources(sc); len(s) != 12 || s[11]["url"] != "https://music.apple.com/ch/song/n1" {
		t.Errorf("sources: %v", s)
	}
	// The mix is read in the owner's storefront, not the configured one.
	if api.mixStorefront != "ch" {
		t.Errorf("mix storefront %q", api.mixStorefront)
	}

	// One section reads only its own lists.
	before := api.called("recs")
	text, _ = callTool(t, h, "/mcp/library", "new_music", `{"section":"recently_added"}`)
	if !strings.Contains(text, "Added 11") || strings.Contains(text, "For You") || api.called("recs") != before {
		t.Errorf("recently_added: %s", text)
	}
	before = api.called("added")
	text, _ = callTool(t, h, "/mcp/library", "new_music", `{"section":"for_you"}`)
	if strings.Contains(text, "Recently added") || !strings.Contains(text, "New Music Mix") || api.called("added") != before {
		t.Errorf("for_you: %s", text)
	}
}

// The New Music Mix is a personal mix (pl.pm-…), in the account's language;
// an editorial playlist with "new music" in its name is not it.
func TestIsNewMusicMix(t *testing.T) {
	for _, tc := range []struct {
		it   applemusic.Item
		want bool
	}{
		{applemusic.Item{Kind: "playlist", ID: "pl.pm-1", Name: "New Music Mix"}, true},
		{applemusic.Item{Kind: "playlist", ID: "pl.pm-2", Name: "Neuheiten-Mix"}, true},
		{applemusic.Item{Kind: "playlist", ID: "pl.123", Name: "New Music Daily"}, false},
		{applemusic.Item{Kind: "playlist", ID: "pl.pm-3", Name: "Chill Mix"}, false},
		{applemusic.Item{Kind: "album", ID: "pl.pm-4", Name: "New Music Mix"}, false},
	} {
		if got := isNewMusicMix(tc.it); got != tc.want {
			t.Errorf("%+v: %v", tc.it, got)
		}
	}
}

// A mix Apple does not know is said so, not reported empty.
func TestNewMusicTool_unknownMix(t *testing.T) {
	_, api, h := connected(t)
	newMusicFixture(api)
	api.listenErr = map[string]error{"mix": applemusic.ErrNotFound}
	text, _ := callTool(t, h, "/mcp/library", "new_music", `{"section":"for_you"}`)
	if !strings.Contains(text, "Apple does not know this mix") || strings.Contains(text, "the mix is empty") {
		t.Errorf("text: %s", text)
	}
}

func TestNewMusicTool_emptyAndErrors(t *testing.T) {
	_, api, h := connected(t)
	text, _ := callTool(t, h, "/mcp/library", "new_music", `{}`)
	if !strings.Contains(text, "(nothing added lately)") || !strings.Contains(text, "(Apple has no picks for the user right now)") {
		t.Errorf("empty: %s", text)
	}

	newMusicFixture(api)
	// One list failing leaves the other.
	api.listenErr = map[string]error{"recs": errors.New("boom")}
	text, _ = callTool(t, h, "/mcp/library", "new_music", `{}`)
	if strings.HasPrefix(text, "ERROR") || !strings.Contains(text, "Added 0") || !strings.Contains(text, "(could not be read right now: boom)") {
		t.Errorf("recs down: %s", text)
	}
	api.listenErr = map[string]error{"recs": errors.New("boom"), "added": errors.New("bang")}
	if text, _ := callTool(t, h, "/mcp/library", "new_music", `{}`); !strings.HasPrefix(text, "ERROR: Apple Music is unavailable right now: bang") {
		t.Errorf("both down: %s", text)
	}
	api.listenErr = map[string]error{"added": errors.New("bang")}
	if text, _ := callTool(t, h, "/mcp/library", "new_music", `{"section":"recently_added"}`); !strings.HasPrefix(text, "ERROR:") {
		t.Errorf("only section down: %s", text)
	}
	// The mix's tracks missing leaves the picks.
	api.listenErr = map[string]error{"mix": errors.New("mix gone")}
	text, _ = callTool(t, h, "/mcp/library", "new_music", `{"section":"for_you"}`)
	if strings.HasPrefix(text, "ERROR") || !strings.Contains(text, "Made for You") || !strings.Contains(text, "(could not be read right now: mix gone)") {
		t.Errorf("mix down: %s", text)
	}
	api.listenErr = nil
	api.mix = nil
	if text, _ := callTool(t, h, "/mcp/library", "new_music", `{"section":"for_you"}`); !strings.Contains(text, "(the mix is empty right now)") {
		t.Errorf("empty mix: %s", text)
	}

	// A refused token ends the call, whichever list saw it.
	for _, tc := range []struct {
		errs map[string]error
		want string
	}{
		{map[string]error{"added": applemusic.ErrUserTokenExpired}, "ERROR: Apple Music sign-in expired"},
		{map[string]error{"recs": applemusic.ErrDevTokenRefused}, "ERROR: Apple Music is unavailable: Apple refused"},
		{map[string]error{"mix": applemusic.ErrDevTokenRefused}, "ERROR: Apple Music is unavailable: Apple refused"},
	} {
		api.listenErr = tc.errs
		if text, _ := callTool(t, h, "/mcp/library", "new_music", `{}`); !strings.HasPrefix(text, tc.want) {
			t.Errorf("%v: %s", tc.errs, text)
		}
	}
	api.listenErr = nil
	for args, want := range map[string]string{
		`{"section":"all"}`: "ERROR: section must be",
		`{"days":3}`:        "ERROR: new_music takes only section",
	} {
		if text, _ := callTool(t, h, "/mcp/library", "new_music", args); !strings.HasPrefix(text, want) {
			t.Errorf("%s: %s", args, text)
		}
	}
}

// A long list stops at the text budget and says how much is left out.
func TestListing_budget(t *testing.T) {
	var l listing
	long := strings.Repeat("x", 1000)
	for range 40 {
		l.add(applemusic.Item{Kind: "song", Name: long}, false, "")
	}
	res := l.result()
	if len(res.Text) > trackTextBudget+100 || !strings.Contains(res.Text, "more not shown.") || res.Sources == nil {
		t.Errorf("len %d, tail %q", len(res.Text), res.Text[len(res.Text)-40:])
	}
}

// A full answer says per section what it lost, and writes no pick headers
// claiming entries it has no room for.
func TestNewMusicTool_fullAnswerNotesPerSection(t *testing.T) {
	_, api, h := connected(t)
	newMusicFixture(api)
	// Ten of these (shown next to the picks) already pass the budget.
	long := strings.Repeat("x", 2500)
	api.added = nil
	for i := range 25 {
		api.added = append(api.added, applemusic.Item{Kind: "album", Name: fmt.Sprintf("%s %d", long, i), Artist: "Band"})
	}
	text, _ := callTool(t, h, "/mcp/library", "new_music", `{"section":"both"}`)
	// The long section says what it lost; short picks still fit and are
	// listed, so their header stays true.
	if !strings.Contains(text, "(2 more in this section not shown: the answer is full)") ||
		!strings.Contains(text, "(8 of 9 listed)") || !strings.Contains(text, "Release 7") {
		t.Errorf("text tail: %s", text[len(text)-600:])
	}

	// Once the text is full, a pick gets no header and its entries count
	// as cut.
	var l listing
	l.b.WriteString(strings.Repeat("x", trackTextBudget))
	res, err := writeForYou(context.Background(), nil, &l, []applemusic.Recommendation{{Title: "Picks", Items: []applemusic.Item{{Kind: "album", Name: "A"}}}}, nil)
	if res != nil || err != nil || strings.Contains(l.b.String(), "Picks:") || !strings.Contains(l.b.String(), "1 more in this section not shown") {
		t.Errorf("full listing: %q", l.b.String()[trackTextBudget:])
	}
}
