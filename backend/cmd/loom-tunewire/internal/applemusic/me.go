package applemusic

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Page sizes per endpoint: Apple caps limit per endpoint and answers a limit
// past the cap with a 400. From Apple's documentation, not checked against
// the live API.
const (
	recentPageSize = 30 // /v1/me/recent/played/tracks
	heavyPageSize  = 10 // /v1/me/history/heavy-rotation
	addedPageSize  = 25 // /v1/me/library/recently-added
	recsPageSize   = 10 // /v1/me/recommendations (the default; the cap is unclear)

	// MaxRecent bounds the recently played tracks read.
	MaxRecent = 50
	// MaxRecommendations bounds the personal recommendations read.
	MaxRecommendations = 2 * recsPageSize
	// MaxMixTracks bounds one catalog playlist's (a personal mix's) read.
	MaxMixTracks = 50
	// MaxLibraryArtists bounds the library artists read: the pages go out one
	// after another.
	MaxLibraryArtists = 5 * pageSize
)

// resource is one entry of a mixed list: songs, albums, playlists, stations,
// catalog and library alike. Every field may be missing.
type resource struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name             string  `json:"name"`
		ArtistName       string  `json:"artistName"`
		CuratorName      string  `json:"curatorName"`
		AlbumName        string  `json:"albumName"`
		ReleaseDate      string  `json:"releaseDate"`
		DateAdded        string  `json:"dateAdded"`
		DurationInMillis int64   `json:"durationInMillis"`
		URL              string  `json:"url"`
		Artwork          artwork `json:"artwork"`
	} `json:"attributes"`
	Relationships struct {
		Artists relation `json:"artists"`
		Catalog relation `json:"catalog"`
	} `json:"relationships"`
}

type relation struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (r relation) first() string {
	if len(r.Data) == 0 {
		return ""
	}
	return r.Data[0].ID
}

// kinds names a resource type the way the tools print it.
var kinds = map[string]string{
	"songs": "song", "library-songs": "song",
	"albums": "album", "library-albums": "album",
	"playlists": "playlist", "library-playlists": "playlist",
	"artists": "artist", "library-artists": "artist",
	"stations":     "station",
	"music-videos": "music video", "library-music-videos": "music video",
}

// item turns r into an Item; ok is false for one without a name. Only an
// https link is kept; a library playlist links to the web player's library.
func (r resource) item() (Item, bool) {
	a := r.Attributes
	if strings.TrimSpace(a.Name) == "" {
		return Item{}, false
	}
	kind, ok := kinds[r.Type]
	if !ok {
		kind = strings.TrimSuffix(strings.TrimPrefix(r.Type, "library-"), "s")
	}
	it := Item{ID: r.ID, Kind: kind, Name: a.Name, Artist: a.ArtistName, Album: a.AlbumName, Released: a.ReleaseDate,
		Added: a.DateAdded, Duration: time.Duration(a.DurationInMillis) * time.Millisecond,
		Artwork: a.Artwork.at(artworkSize)}
	// Only a catalog resource's artists are catalog artists: a library
	// resource's relationship holds library ids (r.…), which the catalog does
	// not know. Catalog artist ids are numeric.
	if id := r.Relationships.Artists.first(); !strings.HasPrefix(r.Type, "library-") && isCatalogID(id) {
		it.ArtistID = id
	}
	if it.Artist == "" {
		it.Artist = a.CuratorName
	}
	switch {
	case strings.HasPrefix(a.URL, "https://"):
		it.URL = a.URL
	case r.Type == "library-playlists" && r.ID != "":
		it.URL = Playlist{ID: r.ID}.URL()
	}
	return it, true
}

// isCatalogID reports whether id looks like a catalog id: digits only.
func isCatalogID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// items reads a paged list of resources, at most max, skipping nameless
// ones.
func (c *Client) items(ctx context.Context, userToken, path string, size, max int) ([]Item, error) {
	var out []Item
	_, err := pages(ctx, c, userToken, path, size, max, func(r resource) {
		if it, ok := r.item(); ok {
			out = append(out, it)
		}
	})
	return out, err
}

// RecentlyPlayed lists the user's recently played tracks, newest first, at
// most MaxRecent.
func (c *Client) RecentlyPlayed(ctx context.Context, userToken string) ([]Item, error) {
	return c.itemsWithArtists(ctx, userToken, "/v1/me/recent/played/tracks", recentPageSize, MaxRecent)
}

// HeavyRotation lists what the user played most lately: albums, playlists,
// stations.
func (c *Client) HeavyRotation(ctx context.Context, userToken string) ([]Item, error) {
	return c.itemsWithArtists(ctx, userToken, "/v1/me/history/heavy-rotation", heavyPageSize, heavyPageSize)
}

// itemsWithArtists reads a list asking Apple to include each entry's
// artists: their catalog ids let new_releases check artists the library does
// not hold. Whether these endpoints take the include is not documented; a 400
// reads the list again without it.
func (c *Client) itemsWithArtists(ctx context.Context, userToken, path string, size, max int) ([]Item, error) {
	items, err := c.items(ctx, userToken, path+"?include=artists", size, max)
	if errors.Is(err, ErrBadRequest) {
		return c.items(ctx, userToken, path, size, max)
	}
	return items, err
}

// RecentlyAdded lists what the user added to the library lately, newest
// first.
func (c *Client) RecentlyAdded(ctx context.Context, userToken string) ([]Item, error) {
	return c.items(ctx, userToken, "/v1/me/library/recently-added", addedPageSize, addedPageSize)
}

// CatalogPlaylistTracks reads a catalog playlist's tracks, at most
// MaxMixTracks. It sends the user token: a personal mix (pl.pm-…) is the
// user's own.
func (c *Client) CatalogPlaylistTracks(ctx context.Context, userToken, storefront, playlistID string) ([]Item, error) {
	path := "/v1/catalog/" + url.PathEscape(storefront) + "/playlists/" + url.PathEscape(playlistID) + "/tracks"
	// A 404 here is a mix Apple does not know (ErrNotFound), not an empty one.
	var out []Item
	// One page of MaxMixTracks: a mix is short, and a second round trip would
	// eat into the tool's deadline.
	_, err := pagesOf(ctx, c, userToken, path, MaxMixTracks, MaxMixTracks, false, func(r resource) {
		if it, ok := r.item(); ok {
			out = append(out, it)
		}
	})
	return out, err
}

// Recommendation is one of Apple's personal picks ("Made for You", "New
// Releases") with what it holds.
type Recommendation struct {
	Title  string
	Reason string
	Items  []Item
}

type recommendation struct {
	Attributes struct {
		Title struct {
			StringForDisplay string `json:"stringForDisplay"`
		} `json:"title"`
		Reason struct {
			StringForDisplay string `json:"stringForDisplay"`
		} `json:"reason"`
	} `json:"attributes"`
	Relationships struct {
		Contents struct {
			Data []resource `json:"data"`
		} `json:"contents"`
		Recommendations struct {
			Data []recommendation `json:"data"`
		} `json:"recommendations"`
	} `json:"relationships"`
}

// flatten turns a recommendation, and the ones a group holds, into the
// list the tools print; an untitled member takes its group's title. Empty
// ones are dropped.
func (r recommendation) flatten(parent string, depth int) []Recommendation {
	title := strings.TrimSpace(r.Attributes.Title.StringForDisplay)
	if title == "" {
		title = parent
	}
	var out []Recommendation
	rec := Recommendation{Title: title, Reason: strings.TrimSpace(r.Attributes.Reason.StringForDisplay)}
	for _, res := range r.Relationships.Contents.Data {
		if it, ok := res.item(); ok {
			rec.Items = append(rec.Items, it)
		}
	}
	if len(rec.Items) > 0 {
		out = append(out, rec)
	}
	if depth < 2 {
		for _, sub := range r.Relationships.Recommendations.Data {
			out = append(out, sub.flatten(title, depth+1)...)
		}
	}
	return out
}

// Recommendations lists Apple's personal recommendations for the user.
func (c *Client) Recommendations(ctx context.Context, userToken string) ([]Recommendation, error) {
	var out []Recommendation
	_, err := pages(ctx, c, userToken, "/v1/me/recommendations", recsPageSize, MaxRecommendations, func(r recommendation) {
		out = append(out, r.flatten("", 0)...)
	})
	return out, err
}

// Artist is one library artist; CatalogID is empty when Apple sent no
// catalog relationship.
type Artist struct {
	ID        string
	Name      string
	CatalogID string
}

// LibraryArtists lists the user's library artists, at most
// MaxLibraryArtists, each with its catalog id when Apple includes it;
// truncated says the library holds more.
func (c *Client) LibraryArtists(ctx context.Context, userToken string) (list []Artist, truncated bool, err error) {
	truncated, err = pages(ctx, c, userToken, "/v1/me/library/artists?include=catalog", pageSize, MaxLibraryArtists, func(r resource) {
		if name := strings.TrimSpace(r.Attributes.Name); name != "" {
			list = append(list, Artist{ID: r.ID, Name: name, CatalogID: r.Relationships.Catalog.first()})
		}
	})
	return list, truncated, err
}

// ArtistCatalogID asks for a library artist's catalog artist; "" when it has
// none (an artist only the user's own files know).
func (c *Client) ArtistCatalogID(ctx context.Context, userToken, libraryID string) (string, error) {
	var rel relation
	err := c.get(ctx, userToken, "/v1/me/library/artists/"+url.PathEscape(libraryID)+"/catalog", &rel)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return rel.first(), err
}

// LatestRelease reads a catalog artist's latest release; ok is false when
// Apple knows none. It needs no user token.
func (c *Client) LatestRelease(ctx context.Context, storefront, artistID string) (Item, bool, error) {
	var page struct {
		Data []resource `json:"data"`
	}
	path := "/v1/catalog/" + url.PathEscape(storefront) + "/artists/" + url.PathEscape(artistID) + "/view/latest-release"
	if err := c.get(ctx, "", path, &page); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Item{}, false, nil
		}
		return Item{}, false, err
	}
	if len(page.Data) == 0 {
		return Item{}, false, nil
	}
	it, ok := page.Data[0].item()
	return it, ok, nil
}
