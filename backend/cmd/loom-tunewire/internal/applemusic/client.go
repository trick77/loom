// Package applemusic reads the Apple Music API the way the music.apple.com web
// player does: the player's developer token, the user's media-user-token for
// library calls, and the player's Origin. It only reads: there is no method
// that sends anything but GET.
package applemusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the API host the web player talks to.
const DefaultBaseURL = "https://amp-api.music.apple.com"

const (
	origin   = "https://music.apple.com"
	pageSize = 100
	// MaxPlaylists bounds one paged read of the library: the pages go out one
	// after another inside one tool call's deadline. Past it the read stops,
	// truncated.
	MaxPlaylists = 20 * pageSize
	// MaxTracks bounds one playlist's read the same way; the tool's text holds
	// about 250 track lines anyway.
	MaxTracks        = 3 * pageSize
	maxResponseBytes = 8 << 20
	// maxRetryAfter caps how long a 429 makes a call wait before its one retry.
	maxRetryAfter = 5 * time.Second
	// probePath is a cheap catalog read that needs only the developer token: it
	// tells a refused developer token from a refused user token.
	probePath = "/v1/storefronts/us"
)

var (
	// ErrUserTokenExpired means Apple refused the media-user-token: expired,
	// revoked, or never valid. Only a new one from the user helps.
	ErrUserTokenExpired = errors.New("apple music refused the media-user-token")
	// ErrDevTokenRefused means Apple refused the web player's developer token
	// even freshly fetched: Apple changed something; the user's token is not at
	// fault.
	ErrDevTokenRefused = errors.New("apple music refused the web player's developer token")
	// ErrNotFound is a 404: no such playlist.
	ErrNotFound = errors.New("apple music: not found")
)

// TokenSource hands out the developer token; *webtoken.Source implements it.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Invalidate(refused string)
}

// Client calls the API. BaseURL and HTTP default when empty.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Dev     TokenSource
	// Sleep waits out a 429; tests replace it.
	Sleep func(context.Context, time.Duration) error
}

// Playlist is one library playlist.
type Playlist struct {
	ID           string
	Name         string
	Description  string
	DateAdded    string
	LastModified string
	Artwork      string
}

// URL opens the playlist in the web player.
func (p Playlist) URL() string { return origin + "/library/playlist/" + url.PathEscape(p.ID) }

// Track is one entry of a library playlist.
type Track struct {
	Name     string
	Artist   string
	Album    string
	Duration time.Duration
}

// Item is one catalog search hit, or one entry of the user's listening lists.
type Item struct {
	ID       string // listening lists only; search leaves it empty
	Kind     string // song, album, artist, playlist, station, music video
	Name     string
	Artist   string
	Album    string
	Released string
	Added    string // library entries: when the user added it
	Duration time.Duration
	URL      string
	Artwork  string
	// ArtistID is the catalog id of the item's (first) artist, when Apple
	// sent the artists relationship.
	ArtistID string
}

// Storefront returns the account's storefront ("ch", "us", …). It doubles as
// the check that a pasted media-user-token works.
func (c *Client) Storefront(ctx context.Context, userToken string) (string, error) {
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.get(ctx, userToken, "/v1/me/storefront", &page); err != nil {
		return "", err
	}
	if len(page.Data) == 0 || page.Data[0].ID == "" {
		return "", errors.New("apple music: storefront response without an id")
	}
	return page.Data[0].ID, nil
}

type playlistAttrs struct {
	Name        string `json:"name"`
	Description struct {
		Standard string `json:"standard"`
	} `json:"description"`
	DateAdded        string  `json:"dateAdded"`
	LastModifiedDate string  `json:"lastModifiedDate"`
	Artwork          artwork `json:"artwork"`
}

func (a playlistAttrs) playlist(id string) Playlist {
	return Playlist{ID: id, Name: a.Name, Description: a.Description.Standard, DateAdded: a.DateAdded,
		LastModified: a.LastModifiedDate, Artwork: a.Artwork.at(artworkSize)}
}

// Playlists lists the user's library playlists, at most MaxPlaylists;
// truncated says the library holds more.
func (c *Client) Playlists(ctx context.Context, userToken string) (list []Playlist, truncated bool, err error) {
	type entry struct {
		ID         string        `json:"id"`
		Attributes playlistAttrs `json:"attributes"`
	}
	truncated, err = pages(ctx, c, userToken, "/v1/me/library/playlists", pageSize, MaxPlaylists, func(e entry) {
		list = append(list, e.Attributes.playlist(e.ID))
	})
	return list, truncated, err
}

// LibrarySize counts the library playlists with one request: Apple's
// meta.total. exact is false when Apple sent no total (the count is then
// unknown); a 404 is an empty library. It doubles as a cheap check that the
// user token reads the library.
func (c *Client) LibrarySize(ctx context.Context, userToken string) (n int, exact bool, err error) {
	var page struct {
		Data []json.RawMessage `json:"data"`
		Next string            `json:"next"`
		Meta struct {
			Total *int `json:"total"`
		} `json:"meta"`
	}
	if err := c.get(ctx, userToken, "/v1/me/library/playlists?limit=1", &page); err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, true, nil
		}
		return 0, false, err
	}
	switch {
	case page.Meta.Total != nil:
		return *page.Meta.Total, true, nil
	case page.Next == "":
		return len(page.Data), true, nil
	default:
		return 0, false, nil
	}
}

// Playlist reads one library playlist by id; ErrNotFound when there is none.
func (c *Client) Playlist(ctx context.Context, userToken, playlistID string) (Playlist, error) {
	var page struct {
		Data []struct {
			ID         string        `json:"id"`
			Attributes playlistAttrs `json:"attributes"`
		} `json:"data"`
	}
	if err := c.get(ctx, userToken, "/v1/me/library/playlists/"+url.PathEscape(playlistID), &page); err != nil {
		return Playlist{}, err
	}
	if len(page.Data) == 0 {
		return Playlist{}, ErrNotFound
	}
	return page.Data[0].Attributes.playlist(page.Data[0].ID), nil
}

// PlaylistTracks lists one library playlist's tracks in order, at most
// MaxTracks; truncated says the playlist holds more.
func (c *Client) PlaylistTracks(ctx context.Context, userToken, playlistID string) (tracks []Track, truncated bool, err error) {
	type attrs struct {
		Name             string `json:"name"`
		ArtistName       string `json:"artistName"`
		AlbumName        string `json:"albumName"`
		DurationInMillis int64  `json:"durationInMillis"`
	}
	type entry struct {
		Attributes attrs `json:"attributes"`
	}
	path := "/v1/me/library/playlists/" + url.PathEscape(playlistID) + "/tracks"
	truncated, err = pages(ctx, c, userToken, path, pageSize, MaxTracks, func(e entry) {
		a := e.Attributes
		tracks = append(tracks, Track{Name: a.Name, Artist: a.ArtistName, Album: a.AlbumName, Duration: time.Duration(a.DurationInMillis) * time.Millisecond})
	})
	return tracks, truncated, err
}

// SearchKinds are the catalog types Search accepts.
var SearchKinds = []string{"songs", "albums", "artists"}

// Search looks term up in a storefront's catalog. It needs no user token.
func (c *Client) Search(ctx context.Context, storefront, term string, kinds []string, limit int) ([]Item, error) {
	q := url.Values{}
	q.Set("term", term)
	q.Set("types", strings.Join(kinds, ","))
	q.Set("limit", strconv.Itoa(limit))
	type hit struct {
		Attributes struct {
			Name             string  `json:"name"`
			ArtistName       string  `json:"artistName"`
			AlbumName        string  `json:"albumName"`
			ReleaseDate      string  `json:"releaseDate"`
			DurationInMillis int64   `json:"durationInMillis"`
			URL              string  `json:"url"`
			Artwork          artwork `json:"artwork"`
		} `json:"attributes"`
	}
	var body struct {
		Results map[string]struct {
			Data []hit `json:"data"`
		} `json:"results"`
	}
	path := "/v1/catalog/" + url.PathEscape(storefront) + "/search?" + q.Encode()
	if err := c.get(ctx, "", path, &body); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Item
	for _, kind := range kinds {
		for _, h := range body.Results[kind].Data {
			a := h.Attributes
			out = append(out, Item{
				Kind: strings.TrimSuffix(kind, "s"), Name: a.Name, Artist: a.ArtistName, Album: a.AlbumName,
				Released: a.ReleaseDate, Duration: time.Duration(a.DurationInMillis) * time.Millisecond, URL: a.URL,
				Artwork: a.Artwork.at(artworkSize),
			})
		}
	}
	return out, nil
}

// artworkSize is the square edge, in pixels, of the cover a source shows.
const artworkSize = 300

// artwork is Apple's image template: a URL with {w} and {h} placeholders.
type artwork struct {
	URL string `json:"url"`
}

// at fills the template for a size×size image; only an https URL is kept.
func (a artwork) at(size int) string {
	if !strings.HasPrefix(a.URL, "https://") {
		return ""
	}
	n := strconv.Itoa(size)
	return strings.NewReplacer("{w}", n, "{h}", n, "{f}", "jpg").Replace(a.URL)
}

// pages follows a resource list's next links, handing each resource to add,
// until the list ends or max resources came in (truncated: there are more).
// size is the endpoint's page size: Apple caps it per endpoint, and a limit
// past the cap is a 400. A 404 on the first page is an empty list (Apple's
// answer for an empty library).
func pages[D any](ctx context.Context, c *Client, userToken, path string, size, max int, add func(D)) (truncated bool, err error) {
	path += sep(path) + "limit=" + strconv.Itoa(size)
	_, query, _ := strings.Cut(path, "?")
	want, _ := url.ParseQuery(query)
	for n, first := 0, true; path != ""; first = false {
		var page struct {
			Data []D    `json:"data"`
			Next string `json:"next"`
		}
		if err := c.get(ctx, userToken, path, &page); err != nil {
			if first && errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		for _, d := range page.Data {
			if n == max {
				return true, nil
			}
			add(d)
			n++
		}
		path = page.Next
		if path != "" && n == max {
			return true, nil
		}
		if path != "" && !strings.HasPrefix(path, "/v1/") {
			return false, fmt.Errorf("apple music: unexpected next link %q", path)
		}
		if path != "" {
			path = carry(path, want)
		}
	}
	return false, nil
}

// carry adds the first request's query parameters (limit, include) that a
// next link left out: Apple's next links carry the offset, not always the
// rest.
func carry(next string, want url.Values) string {
	_, query, _ := strings.Cut(next, "?")
	have, _ := url.ParseQuery(query)
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !have.Has(k) {
			next += sep(next) + url.QueryEscape(k) + "=" + url.QueryEscape(want.Get(k))
		}
	}
	return next
}

func sep(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

// get fetches path into out. A 401 or 403 has two suspects. With a user
// token, a probe with the developer token alone decides: it passes, so the
// user token is refused (ErrUserTokenExpired) and the developer token stays;
// it fails, so the developer token is refetched once and the call retried.
// Refused again (or without a user token), it is ErrDevTokenRefused. A 429
// waits once and retries.
func (c *Client) get(ctx context.Context, userToken, path string, out any) error {
	refetched, waited := false, false
	for {
		dev, err := c.Dev.Token(ctx)
		if err != nil {
			return err
		}
		status, body, retryAfter, err := c.do(ctx, dev, userToken, path)
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("apple music: decode %s: %w", redact(path), err)
			}
			return nil
		case status == http.StatusNotFound:
			return ErrNotFound
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			if userToken != "" {
				switch c.probeDevToken(ctx, dev) {
				case devTokenAccepted:
					return ErrUserTokenExpired
				case devTokenUnknown:
					// A 429, a 5xx or a network blip on the probe proves nothing:
					// keep the developer token, blame neither.
					return fmt.Errorf("apple music: %s returned %d and the check of the developer token failed", redact(path), status)
				}
			}
			if refetched {
				return ErrDevTokenRefused
			}
			c.Dev.Invalidate(dev)
			refetched = true
		case status == http.StatusTooManyRequests && !waited:
			waited = true
			if err := c.sleep(ctx, retryAfter); err != nil {
				return err
			}
		default:
			return fmt.Errorf("apple music: %s returned %d: %s", redact(path), status, apiError(body))
		}
	}
}

type probeResult int

const (
	devTokenAccepted probeResult = iota
	devTokenRefused
	devTokenUnknown
)

// probeDevToken asks Apple whether it accepts dev on a read that needs no user
// token. Only a 401/403 counts as refused; anything else but a 200 is unknown.
func (c *Client) probeDevToken(ctx context.Context, dev string) probeResult {
	status, _, _, err := c.do(ctx, dev, "", probePath)
	switch {
	case err != nil:
		return devTokenUnknown
	case status == http.StatusOK:
		return devTokenAccepted
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return devTokenRefused
	default:
		return devTokenUnknown
	}
}

func (c *Client) do(ctx context.Context, dev, userToken, path string) (int, []byte, time.Duration, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+dev)
	if userToken != "" {
		req.Header.Set("Media-User-Token", userToken)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("apple music: %s: %w", redact(path), err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, 0, err
	}
	return resp.StatusCode, body, retryAfter(resp.Header.Get("Retry-After")), nil
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return min(time.Duration(s)*time.Second, maxRetryAfter)
	}
	return 2 * time.Second
}

// redact drops the query: a search term is the user's words and stays out of
// error text and logs.
func redact(path string) string {
	p, _, _ := strings.Cut(path, "?")
	return p
}

// apiError pulls Apple's error title out of a response body.
func apiError(body []byte) string {
	var e struct {
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		if e.Errors[0].Detail != "" {
			return e.Errors[0].Title + ": " + e.Errors[0].Detail
		}
		return e.Errors[0].Title
	}
	return "no error detail"
}
