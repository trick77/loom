package httpapi

import (
	"context"
	"encoding/json"
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
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/ship/kit/httpx"
)

// LocationLLM reads where the user is from their facts; *llm.Client is one.
type LocationLLM interface {
	ResolveUserLocation(ctx context.Context, facts []llm.FactInput, today string) (llm.UserLocation, error)
}

// geocodeTool turns a place name into a town's centre (docs/weather.md).
var geocodeTool = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherGeocodeTool)

// locationTimeout bounds one derivation: a gate call and a few lookups.
const locationTimeout = 45 * time.Second

// homeFactPrefix starts the fact the start page writes for "where do you live?".
const homeFactPrefix = "Lives "

// locationOn reports whether places can be derived: a store, the model, and
// the weather sidecar's lookup.
func (s *server) locationOn() bool {
	return s.locations != nil && s.locationLLM != nil && s.mcp != nil && s.mcp.HasTool(geocodeTool)
}

// userToday is the user's date, in their kept timezone (UTC without one).
func userToday(user auth.User, now time.Time) string {
	now = now.UTC()
	if loc, err := time.LoadLocation(user.Timezone); user.Timezone != "" && err == nil {
		now = now.In(loc)
	}
	return now.Format(time.DateOnly)
}

// resolveLocation derives the user's places from their memory when the facts
// changed since the last time: a fact added, edited, forgotten or seen again
// on another day all count, whichever path changed it. Unchanged facts cost
// two queries. One derivation per user runs at a time.
func (s *server) resolveLocation(ctx context.Context, user auth.User) error {
	if !s.locationOn() || s.thread == nil {
		return nil
	}
	release, ok := s.inflight.tryAcquire("location:" + user.ID)
	if !ok {
		return nil
	}
	defer release()
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return err
	}
	hash := location.HashFacts(facts)
	if kept, err := s.locations.FactsHash(ctx, user.ID); err != nil || kept == hash {
		return err
	}
	if len(facts) == 0 {
		return s.locations.Save(ctx, user.ID, hash, nil, nil)
	}
	home, current, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, locationTimeout)
	defer cancel()
	inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "user_location", Round: 1}
	read, err := s.locationLLM.ResolveUserLocation(llm.WithInferenceMetadata(ctx, inference), factInputs(facts), userToday(user, s.clock()))
	if err != nil {
		return fmt.Errorf("read location: %w", err)
	}
	newHome, err := s.placeFor(ctx, user, location.KindHome, read.Home, home)
	if err != nil {
		return err
	}
	newCurrent, err := s.placeFor(ctx, user, location.KindCurrent, read.Current, current)
	if err != nil {
		return err
	}
	slog.Info("user location derived", "user_id", user.ID, "home", newHome != nil, "current", newCurrent != nil)
	return s.locations.Save(ctx, user.ID, hash, newHome, newCurrent)
}

// placeFor turns a place the facts name into a town's centre. One kept from
// the same fact, looked up by the same name and country, is reused without a
// lookup; a name the lookup cannot find is no place.
func (s *server) placeFor(ctx context.Context, user auth.User, kind string, ref *llm.PlaceRef, kept *location.Place) (*location.Place, error) {
	if ref == nil {
		return nil, nil
	}
	query := placeQuery(ref.Place, ref.Country)
	if kept != nil && kept.FactID == ref.FactID && kept.Query == query {
		p := *kept
		p.Kind, p.Since, p.Until = kind, ref.Since, ref.Until
		return &p, nil
	}
	p, err := s.geocode(ctx, user, ref.Place, ref.Country)
	if err != nil || p == nil {
		return nil, err
	}
	p.Kind, p.Since, p.Until, p.FactID = kind, ref.Since, ref.Until, ref.FactID
	return p, nil
}

// placeQuery is what a place was looked up by.
func placeQuery(place, country string) string {
	return strings.ToLower(place) + "|" + strings.ToUpper(country)
}

// geocode asks the weather sidecar for the place's centre; nil when it knows
// none of that name. A country the model named narrows the lookup; when it
// finds nothing there, the name alone is looked up.
func (s *server) geocode(ctx context.Context, user auth.User, query, country string) (*location.Place, error) {
	p, err := s.lookUp(ctx, user, query, country)
	if err == nil && p == nil && country != "" {
		p, err = s.lookUp(ctx, user, query, "")
	}
	if p != nil {
		p.Query = placeQuery(query, country)
	}
	return p, err
}

func (s *server) lookUp(ctx context.Context, user auth.User, query, country string) (*location.Place, error) {
	args := map[string]any{"query": query, "language": geocodeLanguage(user)}
	if country != "" {
		args["country"] = country
	}
	text, err := s.mcp.CallTool(ctx, geocodeTool, args)
	if err != nil {
		return nil, fmt.Errorf("look up %q: %w", query, err)
	}
	var reply struct {
		Places []struct {
			Name      string  `json:"name"`
			Region    string  `json:"region"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"places"`
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, fmt.Errorf("look up %q: unreadable reply: %w", query, err)
	}
	if len(reply.Places) == 0 {
		return nil, nil
	}
	m := reply.Places[0]
	return &location.Place{Name: m.Name, Region: m.Region, Country: m.Country, Latitude: m.Latitude, Longitude: m.Longitude, Timezone: m.Timezone}, nil
}

// geocodeLanguage names places in the user's language: the base of their
// pinned one, else English.
func geocodeLanguage(user auth.User) string {
	lang, _, _ := strings.Cut(strings.ToLower(user.ResponseLanguage), "-")
	if len(lang) != 2 {
		return "en"
	}
	return lang
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
	// is saved now. A derivation that read the facts before it saves under
	// their old hash, so the next one reads the new fact.
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

// rememberHome keeps the home as a fact: the one naming the home so far is
// rewritten, an identical one left as it is, else a new one is added.
func (s *server) rememberHome(ctx context.Context, user auth.User, home *location.Place) (chat.UserFact, error) {
	name := home.Name
	if home.Country != "" {
		name += ", " + home.Country
	}
	content := homeFactPrefix + name
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
		if kept != nil && f.ID == kept.FactID {
			updated, found, err := s.thread.UpdateUserFact(ctx, user.ID, "", f.ID, content)
			if err != nil || found {
				return updated, err
			}
		}
	}
	return s.thread.AddUserFact(ctx, user.ID, "", chat.FactCategoryPersonal, content, true)
}

// userPlace is where the user is today; nil when no place is known.
func (s *server) userPlace(ctx context.Context, user auth.User) (*location.Place, error) {
	if s.locations == nil {
		return nil, errors.New("locations are off")
	}
	home, current, _, err := s.locations.Places(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return location.Here(home, current, userToday(user, s.clock())), nil
}
