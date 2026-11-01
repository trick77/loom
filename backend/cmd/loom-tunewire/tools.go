package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/cmd/loom-tunewire/internal/mcpserver"
)

const (
	// maxSources is loom's cap on sources per tool result: items past it get
	// no [n] marker, since they could not be cited.
	maxSources = 20
	// maxNameHits is how many names a miss or an ambiguous match lists.
	maxNameHits  = 30
	defaultLimit = 5
	maxLimit     = 10
	label        = "Apple Music"
)

const (
	msgNotConnected = "Apple Music is not connected: the user can paste their media-user-token in loom's settings (Settings → Apple Music)."
	msgExpired      = "Apple Music sign-in expired: the user needs to paste a new media-user-token in loom's settings (Settings → Apple Music)."
	msgDevRefused   = "Apple Music is unavailable: Apple refused the web player's access token, so loom's Apple Music bridge needs fixing. The user's own sign-in is not the problem."
)

func libraryServer(svc *service, version string) mcpserver.Server {
	return mcpserver.Server{
		Name:    "tunewire",
		Version: version,
		Instructions: "The user's own Apple Music library and listening. list_playlists lists their playlists; " +
			"get_playlist reads one playlist's tracks by id or name; recently_played answers \"what have I been " +
			"listening to\"; new_music answers \"what's new for me\" (recently added, Apple's picks, New Music Mix); " +
			"new_releases answers \"anything new from my bands\". Cite the Apple Music links they return.",
		Tools: []mcpserver.Tool{
			{
				Name:        "list_playlists",
				Title:       "List my Apple Music playlists",
				Description: "List the user's own Apple Music library playlists: name, id, description, last change.",
				Schema:      mcpserver.ObjectSchema(map[string]any{}),
				Run:         func(ctx context.Context, _ json.RawMessage) mcpserver.Result { return runListPlaylists(ctx, svc) },
			},
			{
				Name:  "get_playlist",
				Title: "Read an Apple Music playlist",
				Description: "Read one of the user's Apple Music library playlists: every track with artist, album " +
					"and length, in playlist order.",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"playlist": map[string]any{"type": "string", "minLength": 1,
						"description": "The playlist's id (p.…) from list_playlists, or its name (case-insensitive, a unique part is enough)."},
				}, "playlist"),
				Run: func(ctx context.Context, args json.RawMessage) mcpserver.Result {
					return runGetPlaylist(ctx, svc, args)
				},
			},
			{
				Name:  "recently_played",
				Title: "What I played lately on Apple Music",
				Description: "The user's recently played Apple Music tracks, newest first (title, artist, album), or " +
					"with kind heavy_rotation the albums and playlists they played most lately. Use it for \"what have " +
					"I been listening to\".",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"kind": map[string]any{"type": "string", "enum": []string{"recent", "heavy_rotation"}, "default": "recent",
						"description": "recent: the last tracks played. heavy_rotation: what was played most lately."},
				}),
				Run: func(ctx context.Context, args json.RawMessage) mcpserver.Result {
					return runRecentlyPlayed(ctx, svc, args)
				},
			},
			{
				Name:  "new_music",
				Title: "What's new for me on Apple Music",
				Description: "What is new for the user on Apple Music: what they added to their library lately, and " +
					"Apple's personal picks for them with the tracks of their New Music Mix. Use it for \"anything new " +
					"for me\" or \"what should I listen to\".",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"section": map[string]any{"type": "string", "enum": []string{"both", "recently_added", "for_you"}, "default": "both",
						"description": "recently_added: the library's latest additions. for_you: Apple's picks and the New Music Mix. Default both."},
				}),
				Run: func(ctx context.Context, args json.RawMessage) mcpserver.Result {
					return runNewMusic(ctx, svc, args)
				},
			},
			{
				Name:  "new_releases",
				Title: "New releases from my artists",
				Description: "New albums and singles from the user's favourite artists: the artists they listen to " +
					"most and their library's artists, newest first. Use it for \"anything new from my bands\".",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"days": map[string]any{"type": "integer", "minimum": 1, "maximum": maxReleaseDays, "default": defaultReleaseDays,
						"description": "How far back a release may be, in days."},
				}),
				Run: func(ctx context.Context, args json.RawMessage) mcpserver.Result {
					return runNewReleases(ctx, svc, args)
				},
			},
		},
	}
}

func catalogServer(svc *service, version string) mcpserver.Server {
	return mcpserver.Server{
		Name:    "applemusic",
		Version: version,
		Instructions: "Search the Apple Music catalog for songs, albums and artists. Every song, album or artist you " +
			"name in an answer gets looked up here and cited with its Apple Music link: never a bare name. One search " +
			"per item (\"title artist\"), several at once.",
		Tools: []mcpserver.Tool{{
			Name:  "search_catalog",
			Title: "Search Apple Music",
			Description: "Search the Apple Music catalog. Returns songs, albums and/or artists with their Apple Music " +
				"links and cover art; cite the links.",
			Schema: mcpserver.ObjectSchema(map[string]any{
				"query": map[string]any{"type": "string", "minLength": 1,
					"description": "What to look for: title, artist, album, or a mix (\"bohemian rhapsody queen\")."},
				"type": map[string]any{"type": "string", "enum": []string{"any", "song", "album", "artist"}, "default": "any",
					"description": "Only this kind of result. Default any."},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLimit, "default": defaultLimit,
					"description": "Results per kind."},
			}, "query"),
			Run: func(ctx context.Context, args json.RawMessage) mcpserver.Result { return runSearch(ctx, svc, args) },
		}},
	}
}

// failure turns a service error into the text the model reads.
func failure(err error) mcpserver.Result {
	switch {
	case errors.Is(err, errNotConnected):
		return mcpserver.ErrorResult(msgNotConnected)
	case errors.Is(err, applemusic.ErrUserTokenExpired):
		return mcpserver.ErrorResult(msgExpired)
	case errors.Is(err, applemusic.ErrDevTokenRefused):
		return mcpserver.ErrorResult(msgDevRefused)
	default:
		return mcpserver.ErrorResult("Apple Music is unavailable right now: " + err.Error())
	}
}

// marker numbers the first maxSources items for citation.
func marker(i int) string {
	if i < maxSources {
		return fmt.Sprintf("[%d]", i+1)
	}
	return "-"
}

func runListPlaylists(ctx context.Context, svc *service) mcpserver.Result {
	list, truncated, err := svc.Playlists(ctx)
	if err != nil {
		return failure(err)
	}
	if len(list) == 0 {
		return mcpserver.Result{Text: "The user's Apple Music library has no playlists."}
	}
	var b strings.Builder
	if truncated {
		fmt.Fprintf(&b, "Apple Music library playlists (the first %d; the library holds more):\n", len(list))
	} else {
		fmt.Fprintf(&b, "Apple Music library playlists (%d):\n", len(list))
	}
	sources := []mcpserver.Source{}
	for i, p := range list {
		fmt.Fprintf(&b, "%s %s (id %s)", marker(i), p.Name, p.ID)
		if d := oneLine(p.Description, 160); d != "" {
			b.WriteString(" — " + d)
		}
		if p.LastModified != "" {
			b.WriteString(" · changed " + day(p.LastModified))
		}
		b.WriteByte('\n')
		if i < maxSources {
			sources = append(sources, playlistSource(p))
		}
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

func playlistSource(p applemusic.Playlist) mcpserver.Source {
	return mcpserver.Source{URL: p.URL(), Title: p.Name, Label: label, Snippet: oneLine(p.Description, 200), Image: p.Artwork}
}

func runGetPlaylist(ctx context.Context, svc *service, args json.RawMessage) mcpserver.Result {
	var a struct {
		Playlist string `json:"playlist"`
	}
	if err := mcpserver.DecodeArgs(args, &a); err != nil || strings.TrimSpace(a.Playlist) == "" {
		return mcpserver.ErrorResult("get_playlist needs a playlist id or name")
	}
	p, res, ok := findPlaylist(ctx, svc, strings.TrimSpace(a.Playlist))
	if !ok {
		return res
	}
	tracks, truncated, err := svc.PlaylistTracks(ctx, p.ID)
	if err != nil {
		return failure(err)
	}
	var b strings.Builder
	if truncated {
		fmt.Fprintf(&b, "[1] Playlist %q (id %s), the first %d tracks (it holds more)", p.Name, p.ID, len(tracks))
	} else {
		fmt.Fprintf(&b, "[1] Playlist %q (id %s), %d tracks", p.Name, p.ID, len(tracks))
	}
	if d := oneLine(p.Description, 300); d != "" {
		b.WriteString("\n" + d)
	}
	b.WriteString("\n\n")
	for i, t := range tracks {
		line := fmt.Sprintf("%d. %s — %s", i+1, t.Name, t.Artist)
		if t.Album != "" {
			line += " · " + t.Album
		}
		if t.Duration > 0 {
			line += " (" + clock(t.Duration) + ")"
		}
		// Stop at the budget and say so: a cut mid-list would leave the
		// header claiming tracks the model never saw.
		if b.Len()+len(line) > trackTextBudget {
			fmt.Fprintf(&b, "… and %d more tracks not shown; ask for a shorter excerpt or another playlist.\n", len(tracks)-i)
			break
		}
		b.WriteString(line + "\n")
	}
	return mcpserver.Result{Text: b.String(), Sources: []mcpserver.Source{playlistSource(p)}}
}

// trackTextBudget keeps get_playlist's text under mcpserver's 24 KB cap, with
// room for the closing note.
const trackTextBudget = 22 << 10

// libraryIDPrefix starts every library playlist id (p.…), as list_playlists
// shows them.
const libraryIDPrefix = "p."

// findPlaylist resolves q to a playlist. An id is read directly, without
// paging through the whole library; a name, or an id that is not there, is
// matched against the list. ok false comes with the result to return.
func findPlaylist(ctx context.Context, svc *service, q string) (applemusic.Playlist, mcpserver.Result, bool) {
	if strings.HasPrefix(q, libraryIDPrefix) && !strings.ContainsAny(q, " \t") {
		p, err := svc.Playlist(ctx, q)
		if err == nil {
			return p, mcpserver.Result{}, true
		}
		if !errors.Is(err, applemusic.ErrNotFound) {
			return applemusic.Playlist{}, failure(err), false
		}
	}
	list, truncated, err := svc.Playlists(ctx)
	if err != nil {
		return applemusic.Playlist{}, failure(err), false
	}
	p, miss := resolvePlaylist(list, q)
	if miss != "" {
		if truncated {
			// The playlist may be past the cut, not missing.
			miss = fmt.Sprintf("Only the first %d playlists of the library could be searched, and none matched %q. "+
				"Ask with the playlist's id (p.…) if the user has it. %s", len(list), q, miss)
		}
		return applemusic.Playlist{}, mcpserver.ErrorResult(miss), false
	}
	return p, mcpserver.Result{}, true
}

// resolvePlaylist finds q by id, then exact name, then a unique part of a
// name. miss explains a failure in words the model can act on.
func resolvePlaylist(list []applemusic.Playlist, q string) (applemusic.Playlist, string) {
	for _, p := range list {
		if p.ID == q {
			return p, ""
		}
	}
	lq := strings.ToLower(q)
	var exact, partial []applemusic.Playlist
	for _, p := range list {
		ln := strings.ToLower(p.Name)
		switch {
		case ln == lq:
			exact = append(exact, p)
		case strings.Contains(ln, lq):
			partial = append(partial, p)
		}
	}
	for _, hits := range [][]applemusic.Playlist{exact, partial} {
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], ""
		default:
			return applemusic.Playlist{}, fmt.Sprintf("%d playlists match %q; call again with one id: %s", len(hits), q, names(hits, true))
		}
	}
	if len(list) == 0 {
		return applemusic.Playlist{}, "The user's Apple Music library has no playlists."
	}
	return applemusic.Playlist{}, fmt.Sprintf("No playlist matches %q. The playlists are: %s", q, names(list, false))
}

func names(list []applemusic.Playlist, withID bool) string {
	parts := make([]string, 0, min(len(list), maxNameHits))
	for i, p := range list {
		if i == maxNameHits {
			parts = append(parts, fmt.Sprintf("… and %d more", len(list)-maxNameHits))
			break
		}
		if withID {
			parts = append(parts, fmt.Sprintf("%q (%s)", p.Name, p.ID))
		} else {
			parts = append(parts, fmt.Sprintf("%q", p.Name))
		}
	}
	return strings.Join(parts, ", ")
}

func runSearch(ctx context.Context, svc *service, args json.RawMessage) mcpserver.Result {
	var a struct {
		Query string `json:"query"`
		Type  string `json:"type"`
		Limit int    `json:"limit"`
	}
	if err := mcpserver.DecodeArgs(args, &a); err != nil || strings.TrimSpace(a.Query) == "" {
		return mcpserver.ErrorResult("search_catalog needs a query")
	}
	kinds := applemusic.SearchKinds
	switch a.Type {
	case "", "any":
	case "song", "album", "artist":
		kinds = []string{a.Type + "s"}
	default:
		return mcpserver.ErrorResult("type must be any, song, album or artist")
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	items, err := svc.Search(ctx, strings.TrimSpace(a.Query), kinds, limit)
	if err != nil {
		return failure(err)
	}
	if len(items) == 0 {
		return mcpserver.Result{Text: fmt.Sprintf("Apple Music (%s storefront) has nothing for %q.", svc.storefront, a.Query)}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Apple Music catalog (%s storefront), results for %q:\n", svc.storefront, a.Query)
	sources := []mcpserver.Source{}
	for _, it := range items {
		title, detail := describe(it)
		// Only a hit with a link is citable; one without gets no [n], so the
		// numbers keep matching the sources.
		mark := "-"
		if it.URL != "" && len(sources) < maxSources {
			mark = marker(len(sources))
			sources = append(sources, mcpserver.Source{URL: it.URL, Title: title, Label: label, Snippet: it.Kind + snippetTail(detail), Image: it.Artwork})
		}
		fmt.Fprintf(&b, "%s %s: %s", mark, it.Kind, title)
		if detail != "" {
			b.WriteString(" · " + detail)
		}
		b.WriteByte('\n')
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

// describe renders a hit as its title line and its details.
func describe(it applemusic.Item) (title, detail string) {
	title = it.Name
	if it.Artist != "" && it.Kind != "artist" {
		title += " — " + it.Artist
	}
	var parts []string
	if it.Album != "" {
		parts = append(parts, it.Album)
	}
	if len(it.Released) >= 4 {
		parts = append(parts, it.Released[:4])
	}
	if it.Duration > 0 {
		parts = append(parts, clock(it.Duration))
	}
	return title, strings.Join(parts, " · ")
}

func snippetTail(detail string) string {
	if detail == "" {
		return ""
	}
	return " · " + detail
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// day shortens an RFC 3339 timestamp to its date.
func day(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("2006-01-02")
	}
	return ts
}

func clock(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
