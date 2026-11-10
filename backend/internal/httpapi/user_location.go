package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/weather"
	"github.com/trick77/ship/kit/httpx"
)

// LocationLLM reads where the user is from their facts; *llm.Client is one.
type LocationLLM interface {
	ResolveUserLocation(ctx context.Context, facts []llm.FactInput, today string) (llm.UserLocation, error)
}

// locationTimeout bounds one derivation: a gate call and a few lookups.
const locationTimeout = 45 * time.Second

// homeFactPrefix starts the fact the start page writes for "where do you live?".
const homeFactPrefix = "Lives "

// locationOn reports whether places can be derived: a store, the model, and
// the weather sidecar's lookup.
func (s *server) locationOn() bool {
	return s.locations != nil && s.locationLLM != nil && s.mcp != nil && s.mcp.HasTool(weather.GeocodeTool)
}

// userToday is the user's date, in their kept timezone (UTC without one).
func userToday(user auth.User, now time.Time) string {
	return now.In(userZone(user)).Format(time.DateOnly)
}

// resolveLocation derives the user's places from their memory when the facts
// changed since the last time: a fact added, edited, forgotten or seen again
// on another day all count, whichever path changed it. Unchanged facts cost
// two queries. One derivation per user runs at a time. A derivation that
// fails is noted by the facts' hash (locationFailed), so the start page can
// still ask where the user lives; the next visit tries again.
func (s *server) resolveLocation(ctx context.Context, user auth.User) error {
	tried, err := s.deriveLocation(ctx, user)
	if err != nil && tried != "" {
		// The facts the derivation read, not the ones there now: facts that
		// changed meanwhile were never tried, and must not count as settled.
		s.locationFailed.Store(user.ID, tried)
	}
	return err
}

// deriveLocation is resolveLocation's work; tried is the hash of the facts it
// read, "" when it read none.
func (s *server) deriveLocation(ctx context.Context, user auth.User) (tried string, err error) {
	if !s.locationOn() || s.thread == nil {
		return "", nil
	}
	release, ok := s.inflight.tryAcquire("location:" + user.ID)
	if !ok {
		return "", nil
	}
	defer release()
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return "", err
	}
	hash := location.HashFacts(facts)
	kept, err := s.locations.FactsHash(ctx, user.ID)
	if err != nil || kept == hash {
		return hash, err
	}
	// Saved only if nobody saved places since they were read here: the user
	// may set a home while the model is still reading the facts.
	save := func(home, current *location.Place) error {
		err := s.locations.SaveIf(ctx, user.ID, kept, hash, home, current)
		if errors.Is(err, location.ErrStale) {
			slog.Info("user location changed during its derivation, left as set", "user_id", user.ID)
			return nil
		}
		return err
	}
	if len(facts) == 0 {
		return hash, save(nil, nil)
	}
	home, current, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		return hash, err
	}
	ctx, cancel := context.WithTimeout(ctx, locationTimeout)
	defer cancel()
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "user_location", Round: 1}
	read, err := s.locationLLM.ResolveUserLocation(llm.WithInferenceMetadata(ctx, inference), factInputs(facts, userZone(user)), userToday(user, s.clock()))
	if err != nil {
		return hash, fmt.Errorf("read location: %w", err)
	}
	newHome, err := s.placeFor(ctx, user, location.KindHome, read.Home, home)
	if err != nil {
		return hash, err
	}
	newCurrent, err := s.placeFor(ctx, user, location.KindCurrent, read.Current, current)
	if err != nil {
		return hash, err
	}
	slog.Info("user location derived", "user_id", user.ID, "home", newHome != nil, "current", newCurrent != nil)
	return hash, save(newHome, newCurrent)
}

// placeFor turns a place the facts name into a town's centre. One kept from
// the same fact, looked up by the same name and country, is reused without a
// lookup; a name the lookup cannot find is no place.
func (s *server) placeFor(ctx context.Context, user auth.User, kind string, ref *llm.PlaceRef, kept *location.Place) (*location.Place, error) {
	if ref == nil {
		return nil, nil
	}
	query := placeQuery(ref.Place, ref.Country)
	// The same town named again is the town kept, without a lookup: a name
	// shared by towns far apart may look up as another. A new name is a move,
	// looked up.
	if kept != nil && (kept.FactID == ref.FactID && kept.Query == query || sameTownName(kept, ref.Place)) {
		p := *kept
		p.Kind, p.Since, p.Until, p.FactID = kind, ref.Since, ref.Until, ref.FactID
		return &p, nil
	}
	p, err := s.geocode(ctx, user, ref.Place, ref.Country)
	if err != nil || p == nil {
		return nil, err
	}
	p.Kind, p.Since, p.Until, p.FactID = kind, ref.Since, ref.Until, ref.FactID
	return p, nil
}

// sameTownName reports whether name names the kept place: its name or the
// one it was looked up by, or either with a qualifier ("Baden" for "Baden
// AG").
func sameTownName(kept *location.Place, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	looked, _, _ := strings.Cut(kept.Query, "|")
	for _, known := range []string{strings.ToLower(kept.Name), looked} {
		if known != "" && (known == name || strings.HasPrefix(known, name+" ") || strings.HasPrefix(name, known+" ")) {
			return true
		}
	}
	return false
}

// placeQuery is what a place was looked up by.
func placeQuery(place, country string) string {
	return strings.ToLower(place) + "|" + strings.ToUpper(country)
}

// geocode asks the weather sidecar for the place's centre; nil when it knows
// none of that name. A country the model named narrows the lookup; when it
// finds nothing there, the name alone is looked up.
func (s *server) geocode(ctx context.Context, user auth.User, query, country string) (*location.Place, error) {
	p, err := weather.Find(ctx, s.mcp, query, country, weather.Language(user.ResponseLanguage))
	if p != nil {
		p.Query = placeQuery(query, country)
	}
	return p, err
}

// maxLocationBody bounds the place a user types.
const maxLocationBody = 1 << 10

// handleSetLocation is the start page's "where do you live?". The place is
// looked up and kept as a fact in the user's memory, the one source of where
// they are: the fact naming their home so far is rewritten rather than a
// second one added. The user named it, so it is the home without asking the
// model; a stay derived before is kept.
func (s *server) handleSetLocation(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if !s.locationOn() || s.thread == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "the weather is not configured")
		return
	}
	var body struct {
		Place string `json:"place"`
	}
	if !httpx.DecodeJSON(w, r, &body, maxLocationBody) {
		return
	}
	query := strings.Join(strings.Fields(body.Place), " ")
	if query == "" || len([]rune(query)) > 100 {
		httpx.WriteError(w, http.StatusBadRequest, "place is required, at most 100 characters")
		return
	}
	ctx := r.Context()
	home, err := s.geocode(ctx, user, query, "")
	if err != nil {
		serverError(w, r, err, "look up place failed")
		return
	}
	if home == nil {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "place not found", "code": "not_found"})
		return
	}
	// No lock against a derivation in flight (the start page starts one on
	// every visit, and it may wait on the model for long): the user's answer
	// is saved now, and a derivation that read the places before finds them
	// changed and drops its own (SaveIf). The next one reads the new fact.
	fact, err := s.rememberHome(ctx, user, home)
	if err != nil {
		serverError(w, r, err, "remember place failed")
		return
	}
	_, current, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		serverError(w, r, err, "read location failed")
		return
	}
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		serverError(w, r, err, "list user facts failed")
		return
	}
	home.Kind, home.FactID = location.KindHome, fact.ID
	if err := s.locations.Save(ctx, user.ID, location.HashFacts(facts), home, current); err != nil {
		serverError(w, r, err, "save location failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"place": home})
}

// homeFactFor is the fact this page writes for a home: "Lives Zürich, Schweiz".
func homeFactFor(p *location.Place) string {
	name := p.Name
	if p.Country != "" {
		name += ", " + p.Country
	}
	return homeFactPrefix + name
}

// rememberHome keeps the home as a fact: an identical one is left as it is;
// the fact the current home came from is rewritten only when it is exactly
// what this page wrote for that home; else a new one is added. A fact the
// model wrote may say more than where the user lives ("Lives Bern with Anna,
// works at X"): it stays, and the model reads the newer fact as the home.
func (s *server) rememberHome(ctx context.Context, user auth.User, home *location.Place) (chat.UserFact, error) {
	content := homeFactFor(home)
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return chat.UserFact{}, err
	}
	kept, _, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		return chat.UserFact{}, err
	}
	for _, f := range facts {
		if f.Content == content {
			return f, nil
		}
	}
	for _, f := range facts {
		if kept != nil && f.ID == kept.FactID && f.Content == homeFactFor(kept) {
			updated, found, err := s.thread.UpdateUserFact(ctx, user.ID, "", f.ID, content)
			if err != nil || found {
				return updated, err
			}
		}
	}
	return s.thread.AddUserFact(ctx, user.ID, "", chat.FactCategoryPersonal, content, true)
}

// enginePlaces is the turn engine's view of the users' places.
type enginePlaces struct {
	s *server
}

func (p enginePlaces) UserPlace(ctx context.Context, user auth.User) (*location.Place, error) {
	return p.s.userPlace(ctx, user)
}

// userPlace is where the user is today; nil when no place is known.
func (s *server) userPlace(ctx context.Context, user auth.User) (*location.Place, error) {
	p, _, err := s.placeHere(ctx, user)
	return p, err
}

// placeHere is where the user is today: the town they located, else the
// place their memory names for today; nil when none is known. credit is the
// located town's map data credit.
func (s *server) placeHere(ctx context.Context, user auth.User) (p *location.Place, credit *location.Credit, err error) {
	if s.locations == nil {
		return nil, nil, errors.New("locations are off")
	}
	located, err := s.locations.Located(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	if located != nil {
		return &located.Place, &located.Credit, nil
	}
	home, current, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return location.Here(home, current, userToday(user, s.clock())), nil, nil
}
