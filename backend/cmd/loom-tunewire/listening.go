package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/cmd/loom-tunewire/internal/mcpserver"
)

const (
	// addedAlongside is how much of the recently added list new_music shows
	// next to Apple's picks; asked alone it shows all of it.
	addedAlongside = 10
	// maxRecItems is how many entries of one recommendation new_music shows.
	maxRecItems = 8
	// personalMixPrefix starts the id of a playlist Apple makes for one user
	// (New Music Mix, Chill Mix, …); editorial playlists ("New Music Daily")
	// have plain pl.… ids.
	personalMixPrefix = "pl.pm-"
)

// newMusicNames finds the New Music Mix among the personal mixes by its name,
// which follows the account's language.
var newMusicNames = []string{"new music", "neuheiten", "neue musik", "nouveautés", "novità", "novedades", "nieuwe muziek"}

// isNewMusicMix reports whether it is the user's New Music Mix: a personal
// mix (by id) named like one.
func isNewMusicMix(it applemusic.Item) bool {
	if it.Kind != "playlist" || !strings.HasPrefix(it.ID, personalMixPrefix) {
		return false
	}
	name := strings.ToLower(it.Name)
	for _, n := range newMusicNames {
		if strings.Contains(name, n) {
			return true
		}
	}
	return false
}

// fatal reports an error that ends a tool whatever else worked: no token, a
// refused token, a refused developer token. Every other call would fail the
// same way.
func fatal(err error) bool {
	return errors.Is(err, errNotConnected) || errors.Is(err, applemusic.ErrUserTokenExpired) ||
		errors.Is(err, applemusic.ErrDevTokenRefused)
}

// listing writes a tool's text: plain lines and numbered items. An item with
// a link gets the next [n] and a source, up to maxSources across the result,
// so sources[i] stays item i+1's. Past the text budget items are counted,
// not written.
type listing struct {
	b       strings.Builder
	sources []mcpserver.Source
	cut     int
	// noted is how much of cut a section note already reported.
	noted int
}

func (l *listing) line(format string, args ...any) {
	fmt.Fprintf(&l.b, format, args...)
	l.b.WriteByte('\n')
}

// full reports whether the text budget is spent.
func (l *listing) full() bool { return l.b.Len() >= trackTextBudget }

// endSection notes how many of the section's items did not fit, so the
// model knows which section lost them. The note is short; the budget keeps
// room for it below mcpserver's cap.
func (l *listing) endSection() {
	if n := l.cut - l.noted; n > 0 {
		l.line("(%d more in this section not shown: the answer is full)", n)
		l.noted = l.cut
	}
}

// add writes it as one line; tail goes after its details.
func (l *listing) add(it applemusic.Item, withKind bool, tail string) {
	title, detail := describe(it)
	if tail != "" {
		detail = strings.TrimPrefix(detail+" · "+tail, " · ")
	}
	text := title
	if detail != "" {
		text += " · " + detail
	}
	if withKind {
		text = it.Kind + ": " + text
	}
	if l.b.Len()+len(text) > trackTextBudget {
		l.cut++
		return
	}
	mark := "-"
	if it.URL != "" && len(l.sources) < maxSources {
		mark = marker(len(l.sources))
		l.sources = append(l.sources, mcpserver.Source{URL: it.URL, Title: title, Label: label,
			Snippet: it.Kind + snippetTail(detail), Image: it.Artwork})
	}
	l.line("%s %s", mark, text)
}

func (l *listing) result() mcpserver.Result {
	if n := l.cut - l.noted; n > 0 {
		l.line("… and %d more not shown.", n)
	}
	sources := l.sources
	if sources == nil {
		sources = []mcpserver.Source{}
	}
	return mcpserver.Result{Text: l.b.String(), Sources: sources}
}

func runRecentlyPlayed(ctx context.Context, svc *service, args json.RawMessage) mcpserver.Result {
	var a struct {
		Kind string `json:"kind"`
	}
	if err := mcpserver.DecodeArgs(args, &a); err != nil {
		return mcpserver.ErrorResult("recently_played takes only kind (recent or heavy_rotation)")
	}
	var l listing
	switch a.Kind {
	case "", "recent":
		items, err := svc.RecentlyPlayed(ctx)
		if err != nil {
			return failure(err)
		}
		if len(items) == 0 {
			return mcpserver.Result{Text: "The user has no recently played tracks on Apple Music."}
		}
		l.line("The user's recently played tracks on Apple Music, newest first (%d):", len(items))
		for _, it := range items {
			l.add(it, false, "")
		}
	case "heavy_rotation":
		items, err := svc.HeavyRotation(ctx)
		if err != nil {
			return failure(err)
		}
		if len(items) == 0 {
			return mcpserver.Result{Text: "Apple Music has no heavy rotation for the user yet."}
		}
		l.line("What the user has played most lately on Apple Music (heavy rotation, %d):", len(items))
		for _, it := range items {
			l.add(it, true, "")
		}
	default:
		return mcpserver.ErrorResult("kind must be recent or heavy_rotation")
	}
	return l.result()
}

func runNewMusic(ctx context.Context, svc *service, args json.RawMessage) mcpserver.Result {
	var a struct {
		Section string `json:"section"`
	}
	if err := mcpserver.DecodeArgs(args, &a); err != nil {
		return mcpserver.ErrorResult("new_music takes only section (both, recently_added or for_you)")
	}
	var added, forYou bool
	switch a.Section {
	case "", "both":
		added, forYou = true, true
	case "recently_added":
		added = true
	case "for_you":
		forYou = true
	default:
		return mcpserver.ErrorResult("section must be both, recently_added or for_you")
	}
	// Both lists at once: one after the other they can outlast the tool's
	// deadline on a slow Apple.
	var (
		wg      sync.WaitGroup
		items   []applemusic.Item
		recs    []applemusic.Recommendation
		addErr  error
		recsErr error
	)
	if added {
		wg.Go(func() { items, addErr = svc.RecentlyAdded(ctx) })
	}
	if forYou {
		wg.Go(func() { recs, recsErr = svc.Recommendations(ctx) })
	}
	wg.Wait()
	for _, err := range []error{addErr, recsErr} {
		if fatal(err) {
			return failure(err)
		}
	}
	var l listing
	l.line("New on Apple Music for the user (%s storefront).", svc.ownerStorefront())
	var failed []error
	if added {
		limit := 0 // all of it
		if forYou {
			limit = addedAlongside
		}
		if err := writeRecentlyAdded(ctx, &l, items, addErr, limit); err != nil {
			failed = append(failed, err)
		}
	}
	if forYou {
		res, err := writeForYou(ctx, svc, &l, recs, recsErr)
		if res != nil {
			return *res
		}
		if err != nil {
			failed = append(failed, err)
		}
	}
	// Nothing could be read: an error, not an empty answer.
	if len(failed) > 0 && len(failed) == btoi(added)+btoi(forYou) {
		return failure(failed[0])
	}
	return l.result()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// writeRecentlyAdded writes the recently added section from what was read;
// an error (never a fatal one here) is noted in the text and returned. limit
// 0 writes every entry.
func writeRecentlyAdded(ctx context.Context, l *listing, items []applemusic.Item, err error, limit int) error {
	l.line("\nRecently added to the user's library, newest first:")
	defer l.endSection()
	switch {
	case err != nil:
		slog.WarnContext(ctx, "tunewire: recently added failed", "err", err)
		l.line("(could not be read right now: %v)", err)
		return err
	case len(items) == 0:
		l.line("(nothing added lately)")
	}
	for i, it := range items {
		if limit > 0 && i == limit {
			l.line("… and %d more; ask for section recently_added to see them.", len(items)-limit)
			break
		}
		tail := ""
		if it.Added != "" {
			tail = "added " + day(it.Added)
		}
		l.add(it, true, tail)
	}
	return nil
}

// writeForYou writes Apple's picks (read already) and, when they hold the
// owner's New Music Mix, its tracks. A fatal error reading the mix comes back
// as the result to return; any other is noted in the text and returned.
func writeForYou(ctx context.Context, svc *service, l *listing, recs []applemusic.Recommendation, err error) (*mcpserver.Result, error) {
	l.line("\nApple's personal picks for the user (For You):")
	switch {
	case err != nil:
		slog.WarnContext(ctx, "tunewire: recommendations failed", "err", err)
		l.line("(could not be read right now: %v)", err)
		return nil, err
	case len(recs) == 0:
		l.line("(Apple has no picks for the user right now)")
	}
	var mix *applemusic.Item
	for _, r := range recs {
		for i := range r.Items {
			if mix == nil && isNewMusicMix(r.Items[i]) {
				mix = &r.Items[i]
			}
		}
		shown := min(len(r.Items), maxRecItems)
		if l.full() {
			// No header claiming entries the answer has no room for.
			l.cut += shown
			continue
		}
		head := r.Title
		if r.Reason != "" {
			head += " — " + r.Reason
		}
		if len(r.Items) > shown {
			head += fmt.Sprintf(" (%d of %d listed)", shown, len(r.Items))
		}
		l.line("%s:", head)
		for _, it := range r.Items[:shown] {
			l.add(it, true, "")
		}
	}
	l.endSection()
	if mix == nil {
		return nil, nil
	}
	if l.full() {
		// No room for the mix: say so in one line instead of a header with
		// nothing under it.
		l.line("(the %s's tracks did not fit; ask for section for_you alone to see them)", mix.Name)
		return nil, nil
	}
	tracks, err := svc.MixTracks(ctx, mix.ID)
	if fatal(err) {
		res := failure(err)
		return &res, nil
	}
	l.line("\nTracks in the user's %s:", mix.Name)
	defer l.endSection()
	switch {
	case errors.Is(err, applemusic.ErrNotFound):
		l.line("(Apple does not know this mix in the %s storefront right now)", svc.ownerStorefront())
	case err != nil:
		// The picks are there; only the mix's tracks are missing.
		slog.WarnContext(ctx, "tunewire: mix tracks failed", "err", err)
		l.line("(could not be read right now: %v)", err)
	case len(tracks) == 0:
		l.line("(the mix is empty right now)")
	}
	for _, it := range tracks {
		l.add(it, false, "")
	}
	return nil, nil
}
