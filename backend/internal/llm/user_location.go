package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/trick77/toolkit/llmwire"
)

// PlaceRef is a place named in the user's facts: a town a map search finds.
type PlaceRef struct {
	Place   string `json:"place"`
	Country string `json:"country"`
	// Until is the last day (YYYY-MM-DD) a temporary stay holds; "" for home.
	Until  string `json:"until"`
	FactID string `json:"fact"`
}

// UserLocation is where the facts say the user lives and, for a while, is.
type UserLocation struct {
	Home    *PlaceRef `json:"home"`
	Current *PlaceRef `json:"current"`
}

const userLocationMaxCompletionTokens = 400

const userLocationSystemPrompt = `You read the facts known about a user and say where they live and where they are now. Reply with ONLY a JSON object: {"home":{"place":"...","country":"..","fact":"<id>"} or null,"current":{"place":"...","country":"..","until":"YYYY-MM-DD","fact":"<id>"} or null}.

- Use only what the facts state. Never guess a place from a language, a job, a team or a timezone.
- home: where the user lives. place: the town or city as a map search finds it (no street, no district unless it is how the town is named). country: its ISO 3166-1 alpha-2 code when the facts tell or the place makes it unambiguous, else "".
- current: only when a fact says the user is staying somewhere else for a while (a trip, a stay, a temporary move) and that stay has not ended before today. until: the stay's last day, reckoned from the fact's last_seen date ("this week" seen on a Tuesday ends that Sunday); with no end given, 7 days after last_seen.
- fact: the id of the fact the place comes from.
- No place known: null.`

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ResolveUserLocation reads home and a current stay from the user's facts.
// It is a gate call: the answer is a place name and a date, never prose. today
// is the user's date (YYYY-MM-DD). An unreadable reply is an error.
func (c *Client) ResolveUserLocation(ctx context.Context, facts []FactInput, today string) (UserLocation, error) {
	var b strings.Builder
	b.WriteString("Today: ")
	b.WriteString(today)
	b.WriteString("\n\nFacts (JSON):\n")
	writeJSON(&b, facts)
	b.WriteString("\n\nJSON:")
	messages := []Message{
		{Role: "system", Content: userLocationSystemPrompt},
		{Role: "user", Content: b.String()},
	}
	reply, err := c.shortGate(ctx, messages, userLocationMaxCompletionTokens, nil)
	if err != nil {
		return UserLocation{}, err
	}
	if reply.Empty {
		return UserLocation{}, errors.New("location reply is empty")
	}
	loc, ok := parseUserLocation(reply.Content, facts)
	if !ok {
		return UserLocation{}, fmt.Errorf("location reply is not a location object")
	}
	slog.Debug("user location read", "home", loc.Home != nil, "current", loc.Current != nil)
	return loc, nil
}

// parseUserLocation reads the reply; a place without a name, from a fact not
// in the list, or a stay without a valid end is dropped rather than trusted.
func parseUserLocation(reply string, facts []FactInput) (UserLocation, bool) {
	raw, found := llmwire.JSONObject(reply)
	if !found {
		return UserLocation{}, false
	}
	var loc UserLocation
	if err := json.Unmarshal([]byte(raw), &loc); err != nil {
		return UserLocation{}, false
	}
	known := map[string]bool{}
	for _, f := range facts {
		known[f.ID] = true
	}
	clean := func(p *PlaceRef, stay bool) *PlaceRef {
		if p == nil {
			return nil
		}
		p.Place = strings.Join(strings.Fields(p.Place), " ")
		p.Country = strings.ToUpper(strings.TrimSpace(p.Country))
		if len(p.Country) != 2 {
			p.Country = ""
		}
		if p.Place == "" || len([]rune(p.Place)) > 100 || !known[p.FactID] {
			return nil
		}
		if !stay {
			p.Until = ""
			return p
		}
		if !datePattern.MatchString(p.Until) {
			return nil
		}
		if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
			return nil
		}
		return p
	}
	loc.Home = clean(loc.Home, false)
	loc.Current = clean(loc.Current, true)
	return loc, true
}
