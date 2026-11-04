package main

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/ship/kit/mcpserver"
)

const (
	defaultReleaseDays = 30
	maxReleaseDays     = 120
	// maxReleaseArtists bounds one call's artist lookups.
	maxReleaseArtists = 50
	// releaseWorkers is how many artist lookups run at once.
	releaseWorkers = 4
	// releaseTTL is how long an artist's latest release is remembered: a
	// follow-up question costs no lookups.
	releaseTTL = 3 * time.Hour
	// maxCachedReleases bounds the memory; far above one call's artists.
	maxCachedReleases = 1000
	// libraryArtistsTTL is how long the library's artist list is reused.
	libraryArtistsTTL = 30 * time.Minute
	// releaseBudget bounds the artist lookups; releaseMargin is kept free
	// before the tool's deadline to write the answer.
	releaseBudget = 20 * time.Second
	releaseMargin = 3 * time.Second
)

// Listening weights: how much one entry of a list says the user likes its
// artist.
const (
	weightPlayed = 1 // per recently played track
	weightAdded  = 2 // per recently added album or song
	weightHeavy  = 3 // per heavy-rotation album
)

// cachedRelease is an artist's latest release; found false when Apple knows
// none. Errors are never cached.
type cachedRelease struct {
	release applemusic.Item
	found   bool
	at      time.Time
}

// releaseCache remembers latest releases per storefront and artist, in
// memory only, bounded.
type releaseCache struct {
	mu sync.Mutex
	m  map[string]cachedRelease
}

func (c *releaseCache) get(key string, now time.Time) (cachedRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || now.Sub(e.at) >= releaseTTL {
		return cachedRelease{}, false
	}
	return e, true
}

// put stores e; when full, expired entries go first, then the oldest.
func (c *releaseCache) put(key string, e cachedRelease) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cachedRelease{}
	}
	if _, ok := c.m[key]; !ok && len(c.m) >= maxCachedReleases {
		for k, v := range c.m {
			if e.at.Sub(v.at) >= releaseTTL {
				delete(c.m, k)
			}
		}
		if len(c.m) >= maxCachedReleases {
			oldest, first := "", true
			for k, v := range c.m {
				if first || v.at.Before(c.m[oldest].at) {
					oldest, first = k, false
				}
			}
			delete(c.m, oldest)
		}
	}
	c.m[key] = e
}

// artistsCache keeps each user's library artist list a while, for the token it
// was read with: paging it is the slow part of new_releases. One entry per
// loom user; no user ever reads another's.
type artistsCache struct {
	mu sync.Mutex
	m  map[string]cachedArtists
}

type cachedArtists struct {
	token     string
	at        time.Time
	list      []applemusic.Artist
	truncated bool
}

// forget drops user's entry (a new or removed token).
func (c *artistsCache) forget(user string) {
	c.mu.Lock()
	delete(c.m, user)
	c.mu.Unlock()
}

// LibraryArtists lists the caller's library artists, from memory when read
// lately with the same token.
func (s *service) LibraryArtists(ctx context.Context) ([]applemusic.Artist, bool, error) {
	user, tok, err := s.userToken(ctx)
	if err != nil {
		return nil, false, err
	}
	c := &s.artists
	c.mu.Lock()
	if e, ok := c.m[user]; ok && e.token == tok && s.now().Sub(e.at) < libraryArtistsTTL {
		c.mu.Unlock()
		return e.list, e.truncated, nil
	}
	c.mu.Unlock()
	list, truncated, err := s.api.LibraryArtists(ctx, tok)
	s.record(user, tok, err, nil)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]cachedArtists{}
	}
	c.m[user] = cachedArtists{token: tok, at: s.now(), list: list, truncated: truncated}
	c.mu.Unlock()
	return list, truncated, nil
}

// candidate is an artist new_releases checks.
type candidate struct {
	name      string
	catalogID string
	libraryID string
	score     int
	order     int
}

// key names the artist in the release cache. A catalog id is shared catalog
// data; a library id is one user's library, mapped with that user's token, so
// its entry is that user's alone.
func (c candidate) key(user string) string {
	if c.catalogID != "" {
		return c.catalogID
	}
	return "l:" + user + ":" + c.libraryID
}

type signal struct {
	items  []applemusic.Item
	weight int
}

// artistNames splits a credit ("A, B & C", "A feat. B") into its artists.
func artistNames(credit string) []string {
	r := strings.NewReplacer(" feat. ", "\x00", " ft. ", "\x00", " & ", "\x00", ", ", "\x00")
	var out []string
	for _, p := range strings.Split(r.Replace(credit), "\x00") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func norm(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// rankArtists picks the artists to check: library artists and artists the
// user listened to, the most listened first, then the library in its own
// order, at most maxReleaseArtists. fromListening counts the picks the
// listening ranked.
func rankArtists(lib []applemusic.Artist, signals ...signal) (top []candidate, fromListening int) {
	byName := map[string]*candidate{}
	byCatalog := map[string]*candidate{}
	var all []*candidate
	add := func(c *candidate) {
		c.order = len(all)
		all = append(all, c)
		byName[norm(c.name)] = c
		if c.catalogID != "" {
			byCatalog[c.catalogID] = c
		}
	}
	for _, a := range lib {
		if byName[norm(a.Name)] == nil {
			add(&candidate{name: a.Name, catalogID: a.CatalogID, libraryID: a.ID})
		}
	}
	for _, s := range signals {
		for _, it := range s.items {
			// A playlist's or station's "artist" is its curator.
			if strings.TrimSpace(it.Artist) == "" || it.Kind == "playlist" || it.Kind == "station" {
				continue
			}
			names := artistNames(it.Artist)
			credited := map[*candidate]bool{}
			credit := func(c *candidate) {
				if c != nil && !credited[c] {
					credited[c] = true
					c.score += s.weight
				}
			}
			// The catalog artist the entry names is its main artist: credit it
			// by id, so "A" and "A & B" are one artist, and add it when the
			// library does not hold it.
			if it.ArtistID != "" {
				c := byCatalog[it.ArtistID]
				if c == nil {
					if c = byName[norm(it.Artist)]; c == nil && len(names) > 0 {
						c = byName[norm(names[0])]
					}
					switch {
					case c != nil && c.catalogID == "":
						c.catalogID = it.ArtistID
						byCatalog[it.ArtistID] = c
					case c != nil && c.catalogID != it.ArtistID:
						// Same name, another catalog artist: two artists, not one.
						c = nil
					}
				}
				if c == nil {
					name := it.Artist
					if len(names) > 0 {
						name = names[0]
					}
					c = &candidate{name: name, catalogID: it.ArtistID}
					c.order = len(all)
					all = append(all, c)
					byCatalog[it.ArtistID] = c
					// By name only when the name is free: a library artist of
					// the same name keeps it.
					if byName[norm(name)] == nil {
						byName[norm(name)] = c
					}
				}
				credit(c)
			} else {
				credit(byName[norm(it.Artist)])
			}
			// Featured artists the library holds count too. With a catalog id
			// the first name is the main artist, credited by id above: by name
			// it could be another artist of the same name.
			featured := names
			if it.ArtistID != "" && len(names) > 0 {
				featured = names[1:]
			}
			for _, part := range featured {
				credit(byName[norm(part)])
			}
		}
	}
	slices.SortStableFunc(all, func(a, b *candidate) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.order, b.order))
	})
	seen := map[string]bool{}
	for _, c := range all {
		if len(top) == maxReleaseArtists {
			break
		}
		// One user's list: the cache key without a user dedupes it.
		if c.catalogID == "" && c.libraryID == "" || seen[c.key("")] {
			continue
		}
		seen[c.key("")] = true
		top = append(top, *c)
		if c.score > 0 {
			fromListening++
		}
	}
	return top, fromListening
}

// outcome is one artist's lookup: done false when time ran out before it.
type outcome struct {
	done    bool
	release applemusic.Item
	found   bool
	err     error
}

// lookupReleases reads each candidate's latest release, releaseWorkers at a
// time, until the budget is spent; what was not reached stays not done.
func (s *service) lookupReleases(ctx context.Context, sf string, cands []candidate) []outcome {
	budget := s.releaseBudget
	if dl, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(dl)-releaseMargin)
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out := make([]outcome, len(cands))
	// Cached answers first: they cost nothing, so a spent budget must not
	// leave them out.
	var todo []int
	for i, c := range cands {
		if hit, ok := s.releases.get(sf+"|"+c.key(mcpserver.User(ctx)), s.now()); ok {
			out[i] = outcome{done: true, release: hit.release, found: hit.found}
			continue
		}
		todo = append(todo, i)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(releaseWorkers, len(todo)) {
		wg.Go(func() {
			for i := range jobs {
				out[i] = s.lookupRelease(ctx, sf, cands[i])
			}
		})
	}
feed:
	for _, i := range todo {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return out
}

// lookupRelease reads one uncached candidate's latest release (lookupReleases
// answers the cached ones) and caches it.
func (s *service) lookupRelease(ctx context.Context, sf string, c candidate) outcome {
	key := sf + "|" + c.key(mcpserver.User(ctx))
	if ctx.Err() != nil {
		return outcome{}
	}
	id := c.catalogID
	if id == "" {
		var err error
		if id, err = s.ArtistCatalogID(ctx, c.libraryID); err != nil {
			return failedLookup(err)
		}
	}
	var it applemusic.Item
	found := false
	if id != "" {
		var err error
		if it, found, err = s.api.LatestRelease(ctx, sf, id); err != nil {
			return failedLookup(err)
		}
	}
	s.releases.put(key, cachedRelease{release: it, found: found, at: s.now()})
	return outcome{done: true, release: it, found: found}
}

// failedLookup: a lookup cut by the deadline was not done; any other failure
// was, and failed.
func failedLookup(err error) outcome {
	if transient(err) {
		return outcome{}
	}
	return outcome{done: true, err: err}
}

// releasedWithin parses an album's release date and reports whether it falls
// in the last days days or later (a pre-release). A date that is only a
// year or a month cannot tell and is left out.
func releasedWithin(released string, now time.Time, days int) bool {
	t, err := time.Parse(time.DateOnly, released)
	if err != nil {
		return false
	}
	today := now.UTC().Truncate(24 * time.Hour)
	return !t.Before(today.AddDate(0, 0, -days))
}

func runNewReleases(ctx context.Context, svc *service, args json.RawMessage) mcpserver.Result {
	var a struct {
		Days int `json:"days"`
	}
	if err := mcpserver.DecodeArgs(args, &a); err != nil {
		return mcpserver.ErrorResult("new_releases takes only days (a number of days)")
	}
	days := a.Days
	if days <= 0 {
		days = defaultReleaseDays
	}
	days = min(days, maxReleaseDays)
	if _, _, err := svc.userToken(ctx); err != nil {
		return failure(err)
	}

	// What the user listens to and the library's artists, at once.
	var (
		wg                   sync.WaitGroup
		recent, heavy, added []applemusic.Item
		lib                  []applemusic.Artist
		libTruncated         bool
		errs                 [4]error
	)
	wg.Go(func() { recent, errs[0] = svc.RecentlyPlayed(ctx) })
	wg.Go(func() { heavy, errs[1] = svc.HeavyRotation(ctx) })
	wg.Go(func() { added, errs[2] = svc.RecentlyAdded(ctx) })
	wg.Go(func() { lib, libTruncated, errs[3] = svc.LibraryArtists(ctx) })
	wg.Wait()
	var (
		soft    error
		missing []string
	)
	inputs := [4]string{"recently played", "heavy rotation", "recently added", "library artists"}
	for i, err := range errs {
		if fatal(err) {
			return failure(err)
		}
		if err != nil {
			// One list missing narrows the artists; the rest still count, and
			// the answer says which one is missing.
			slog.WarnContext(ctx, "tunewire: new_releases input failed", "input", inputs[i], "err", err)
			soft = cmp.Or(soft, err)
			missing = append(missing, inputs[i])
		}
	}
	cands, fromListening := rankArtists(lib,
		signal{recent, weightPlayed}, signal{heavy, weightHeavy}, signal{added, weightAdded})
	if len(cands) == 0 {
		if soft != nil {
			return failure(soft)
		}
		return mcpserver.Result{Text: "The user's Apple Music library has no artists and there is no recent listening to go by: no favourite artists to check."}
	}

	sf := svc.ownerStorefront(ctx)
	outs := svc.lookupReleases(ctx, sf, cands)
	var (
		releases        []applemusic.Item
		checked, failed int
		seen            = map[string]bool{}
	)
	for _, o := range outs {
		if fatal(o.err) {
			return failure(o.err)
		}
		if !o.done {
			continue
		}
		checked++
		if o.err != nil {
			failed++
			continue
		}
		key := cmp.Or(o.release.URL, norm(o.release.Name+"\x00"+o.release.Artist))
		if o.found && releasedWithin(o.release.Released, svc.now(), days) && !seen[key] {
			seen[key] = true
			releases = append(releases, o.release)
		}
	}
	slices.SortStableFunc(releases, func(x, y applemusic.Item) int { return strings.Compare(y.Released, x.Released) })

	var l listing
	l.line("New releases from the user's artists on Apple Music (%s storefront), last %d days.", sf, days)
	from := ""
	if more := len(cands) - fromListening; more > 0 {
		from = ", " + plural(more, "more artist") + " from the library in library order"
		if libTruncated {
			from += " (only its first " + plural(len(lib), "artist") + " were read)"
		}
	}
	l.line("Artists picked: %s, %d by recent listening%s.", plural(len(cands), "artist"), fromListening, from)
	// The caveats come before the list: a long list cannot push them past
	// the text cap, and the model reads them before the results.
	if len(missing) > 0 {
		l.line("Incomplete: Apple could not give the user's %s right now, so artists from there may be missing.", strings.Join(missing, ", "))
	}
	if unchecked := len(cands) - checked; unchecked > 0 {
		l.line("Partial list: only %d of the %d artists could be checked in time. Asking again checks the rest; the ones done are remembered for a few hours.", checked, len(cands))
	}
	if failed > 0 {
		l.line("%s could not be checked: Apple answered with an error.", plural(failed, "artist"))
	}
	if len(releases) == 0 {
		l.line("None of the %s checked released anything in the last %d days.", plural(checked-failed, "artist"), days)
	}
	today := svc.now().UTC().Format(time.DateOnly)
	for _, r := range releases {
		tail := "released " + r.Released
		if r.Released > today {
			tail = "out " + r.Released + " (pre-release)"
		}
		r.Released = "" // in the tail, not as a year
		l.add(r, false, tail)
	}
	return l.result()
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
