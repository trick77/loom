package applemusic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecentlyPlayed_pagesAtThirtyAndSkipsNamelessItems(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "" {
			_, _ = fmt.Fprint(w, `{"next":"/v1/me/recent/played/tracks?offset=30","data":[
				{"id":"1","type":"songs","attributes":{"name":"Song","artistName":"Band","albumName":"LP","releaseDate":"2026-10-01","durationInMillis":225000,"url":"https://music.apple.com/ch/song/1","artwork":{"url":"https://img/{w}x{h}.jpg"}},
					"relationships":{"artists":{"data":[{"id":"909","type":"artists"}]}}},
				{"id":"i.2","type":"library-songs","attributes":{"name":"Own Rip","artistName":"Me"},
					"relationships":{"artists":{"data":[{"id":"r.1","type":"library-artists"}]}}},
				{"id":"3","type":"songs","attributes":{}},
				{"id":"4","type":"songs"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"5","type":"music-videos","attributes":{"name":"Clip","artistName":"Band","url":"http://insecure/5"}}]}`)
	})
	got, err := c.RecentlyPlayed(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("items = %+v", got)
	}
	want := Item{ID: "1", Kind: "song", Name: "Song", Artist: "Band", Album: "LP", Released: "2026-10-01", Duration: 225 * time.Second,
		URL: "https://music.apple.com/ch/song/1", Artwork: "https://img/300x300.jpg", ArtistID: "909"}
	if got[0] != want {
		t.Errorf("song = %+v", got[0])
	}
	// A library resource's artist id is a library id: not a catalog artist.
	if got[1].ArtistID != "" {
		t.Errorf("library song kept artist id %q", got[1].ArtistID)
	}
	if got[1].Kind != "song" || got[1].URL != "" || got[2].Kind != "music video" || got[2].URL != "" {
		t.Errorf("library song / video = %+v %+v", got[1], got[2])
	}
	r := *reqs
	if len(r) != 2 || r[0].path != "/v1/me/recent/played/tracks?include=artists&limit=30" || r[1].path != "/v1/me/recent/played/tracks?offset=30&include=artists&limit=30" {
		t.Errorf("requests %+v", r)
	}
	if r[0].user != "u" {
		t.Errorf("no user token: %+v", r[0])
	}
}

func TestRecentlyPlayed_capsAtMaxRecent(t *testing.T) {
	c, _, _ := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		for i := range recentPageSize {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":"%d","type":"songs","attributes":{"name":"n"}}`, i)
		}
		_, _ = fmt.Fprintf(w, `{"next":"%s?offset=1","data":[%s]}`, r.URL.Path, b.String())
	})
	got, err := c.RecentlyPlayed(context.Background(), "u")
	if err != nil || len(got) != MaxRecent {
		t.Fatalf("%d items, %v", len(got), err)
	}
}

func TestHeavyRotationAndRecentlyAdded_mixedKinds(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/history/heavy-rotation":
			_, _ = fmt.Fprint(w, `{"data":[
				{"id":"1","type":"albums","attributes":{"name":"LP","artistName":"Band","url":"https://music.apple.com/ch/album/1"}},
				{"id":"p.X","type":"library-playlists","attributes":{"name":"Mine"}},
				{"id":"pl.1","type":"playlists","attributes":{"name":"Hits","curatorName":"Apple Music","url":"https://music.apple.com/ch/playlist/pl.1"}},
				{"id":"ra.1","type":"stations","attributes":{"name":"Band Radio","url":"https://music.apple.com/ch/station/ra.1"}},
				{"id":"x","type":"uploaded-audios","attributes":{"name":"Odd"}}]}`)
		case "/v1/me/library/recently-added":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"l.1","type":"library-albums","attributes":{"name":"New LP","artistName":"Band","dateAdded":"2026-10-30T08:00:00Z"}}]}`)
		}
	})
	heavy, err := c.HeavyRotation(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, it := range heavy {
		kinds = append(kinds, it.Kind+":"+it.Artist+":"+it.URL)
	}
	if got := strings.Join(kinds, " | "); got != "album:Band:https://music.apple.com/ch/album/1 | playlist::https://music.apple.com/library/playlist/p.X | "+
		"playlist:Apple Music:https://music.apple.com/ch/playlist/pl.1 | station::https://music.apple.com/ch/station/ra.1 | uploaded-audio::" {
		t.Errorf("heavy = %s", got)
	}
	added, err := c.RecentlyAdded(context.Background(), "u")
	if err != nil || len(added) != 1 || added[0].Kind != "album" || added[0].Added != "2026-10-30T08:00:00Z" {
		t.Fatalf("added = %+v, %v", added, err)
	}
	if p := (*reqs)[0].path; p != "/v1/me/history/heavy-rotation?include=artists&limit=10" {
		t.Errorf("heavy path %s", p)
	}
	if p := (*reqs)[1].path; p != "/v1/me/library/recently-added?limit=25" {
		t.Errorf("added path %s", p)
	}
}

func TestListening_404IsEmptyAndAuthErrorsPass(t *testing.T) {
	c, _, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	for name, call := range map[string]func() ([]Item, error){
		"recent": func() ([]Item, error) { return c.RecentlyPlayed(context.Background(), "u") },
		"heavy":  func() ([]Item, error) { return c.HeavyRotation(context.Background(), "u") },
		"added":  func() ([]Item, error) { return c.RecentlyAdded(context.Background(), "u") },
	} {
		if got, err := call(); err != nil || len(got) != 0 {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	// A mix Apple does not know is not an empty mix.
	if _, err := c.CatalogPlaylistTracks(context.Background(), "u", "ch", "pl.pm-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("mix 404: %v, want ErrNotFound", err)
	}
	recs, err := c.Recommendations(context.Background(), "u")
	if err != nil || len(recs) != 0 {
		t.Errorf("recs: %v %v", recs, err)
	}
	c, _, _ = newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == probePath {
			_, _ = fmt.Fprint(w, `{"data":[]}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := c.RecentlyPlayed(context.Background(), "u"); !errors.Is(err, ErrUserTokenExpired) {
		t.Errorf("expired: %v", err)
	}
	if _, err := c.Recommendations(context.Background(), "u"); !errors.Is(err, ErrUserTokenExpired) {
		t.Errorf("expired recs: %v", err)
	}
}

func TestRecommendations_flattensGroupsAndKeepsTitles(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[
			{"id":"r1","type":"personal-recommendation","attributes":{"title":{"stringForDisplay":"Made for You"},"reason":{"stringForDisplay":"Updated Fridays"},"isGroupRecommendation":false},
				"relationships":{"contents":{"data":[
					{"id":"pl.pm-1","type":"playlists","attributes":{"name":"New Music Mix","curatorName":"Apple Music","url":"https://music.apple.com/ch/playlist/new-music-mix/pl.pm-1"}},
					{"id":"pl.pm-2","type":"playlists"}]}}},
			{"id":"r2","type":"personal-recommendation","attributes":{"title":{"stringForDisplay":"New Releases"},"isGroupRecommendation":true},
				"relationships":{"recommendations":{"data":[
					{"id":"r2a","attributes":{"title":{"stringForDisplay":""}},"relationships":{"contents":{"data":[
						{"id":"9","type":"albums","attributes":{"name":"Fresh","artistName":"Band","releaseDate":"2026-10-28","url":"https://music.apple.com/ch/album/9"}}]}}},
					{"id":"r2b","attributes":{"title":{"stringForDisplay":"Empty"}}}]}}},
			{"id":"r3","attributes":{"title":{"stringForDisplay":"Nothing inside"}}}]}`)
	})
	recs, err := c.Recommendations(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Title != "Made for You" || recs[0].Reason != "Updated Fridays" || len(recs[0].Items) != 1 ||
		recs[1].Title != "New Releases" || recs[1].Items[0].Name != "Fresh" {
		t.Fatalf("recs = %+v", recs)
	}
	if p := (*reqs)[0].path; p != "/v1/me/recommendations?limit=10" {
		t.Errorf("path %s", p)
	}
}

func TestCatalogPlaylistTracks_sendsTheUserTokenAndEscapes(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"1","type":"songs","attributes":{"name":"New One","artistName":"Band","url":"https://music.apple.com/ch/song/1"}}]}`)
	})
	got, err := c.CatalogPlaylistTracks(context.Background(), "u", "ch", "pl.pm/x")
	if err != nil || len(got) != 1 || got[0].Name != "New One" {
		t.Fatalf("%+v %v", got, err)
	}
	if q := (*reqs)[0]; q.user != "u" || q.path != "/v1/catalog/ch/playlists/pl.pm%2Fx/tracks?limit=50" {
		t.Errorf("request %+v", q)
	}
}

// Apple refuses include=artists with a 400: the list is read again without
// it, not lost.
func TestRecentlyPlayed_withoutIncludeOn400(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"errors":[{"title":"Invalid Parameter Value"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"1","type":"songs","attributes":{"name":"Song"}}]}`)
	})
	got, err := c.RecentlyPlayed(context.Background(), "u")
	if err != nil || len(got) != 1 || len(*reqs) != 2 || (*reqs)[1].path != "/v1/me/recent/played/tracks?limit=30" {
		t.Fatalf("%+v %v %+v", got, err, *reqs)
	}
}

func TestLibraryArtists_keepsIncludeAcrossPages(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "" {
			_, _ = fmt.Fprint(w, `{"next":"/v1/me/library/artists?offset=100","data":[
				{"id":"r.1","type":"library-artists","attributes":{"name":"Band"},"relationships":{"catalog":{"data":[{"id":"111","type":"artists"}]}}},
				{"id":"r.2","type":"library-artists","attributes":{"name":"Other"}},
				{"id":"r.3","type":"library-artists","attributes":{"name":""}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"r.4","attributes":{"name":"Late"},"relationships":{"catalog":{"data":[]}}}]}`)
	})
	got, truncated, err := c.LibraryArtists(context.Background(), "u")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	want := []Artist{{ID: "r.1", Name: "Band", CatalogID: "111"}, {ID: "r.2", Name: "Other"}, {ID: "r.4", Name: "Late"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("artists = %+v", got)
	}
	r := *reqs
	if len(r) != 2 || r[0].path != "/v1/me/library/artists?include=catalog&limit=100" ||
		r[1].path != "/v1/me/library/artists?offset=100&include=catalog&limit=100" {
		t.Errorf("requests %+v", r)
	}
}

func TestArtistCatalogID(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/library/artists/r.1/catalog":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"111","type":"artists"}]}`)
		case "/v1/me/library/artists/r.2/catalog":
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	for lib, want := range map[string]string{"r.1": "111", "r.2": "", "r.3": ""} {
		if got, err := c.ArtistCatalogID(context.Background(), "u", lib); err != nil || got != want {
			t.Errorf("%s: %q %v", lib, got, err)
		}
	}
	if (*reqs)[0].user != "u" {
		t.Error("no user token on the library relationship")
	}
}

func TestLatestRelease(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/ch/artists/111/view/latest-release":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"9","type":"albums","attributes":{"name":"Fresh - Single","artistName":"Band","releaseDate":"2026-10-28",
				"url":"https://music.apple.com/ch/album/9","artwork":{"url":"https://img/{w}x{h}bb.{f}"}}}]}`)
		case "/v1/catalog/ch/artists/222/view/latest-release":
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		case "/v1/catalog/ch/artists/333/view/latest-release":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"8","type":"albums","attributes":{}}]}`)
		case "/v1/catalog/ch/artists/500/view/latest-release":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	it, ok, err := c.LatestRelease(context.Background(), "ch", "111")
	if err != nil || !ok || it.Name != "Fresh - Single" || it.Kind != "album" || it.Released != "2026-10-28" || it.Artwork != "https://img/300x300bb.jpg" {
		t.Fatalf("%+v %v %v", it, ok, err)
	}
	if q := (*reqs)[0]; q.user != "" {
		t.Errorf("catalog call sent the user token: %+v", q)
	}
	for _, id := range []string{"222", "333", "404"} {
		if _, ok, err := c.LatestRelease(context.Background(), "ch", id); ok || err != nil {
			t.Errorf("%s: %v %v", id, ok, err)
		}
	}
	if _, _, err := c.LatestRelease(context.Background(), "ch", "500"); err == nil {
		t.Error("500 passed")
	}
}
