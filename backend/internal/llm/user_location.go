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
	// Since and Until are the first and last day (YYYY-MM-DD) a stay holds;
	// "" for home. Since may lie ahead: a planned trip.
	Since  string `json:"since"`
	Until  string `json:"until"`
	FactID string `json:"fact"`
}

// UserLocation is where the facts say the user lives and, for a while, is.
type UserLocation struct {
	Home    *PlaceRef `json:"home"`
	Current *PlaceRef `json:"current"`
}

const userLocationMaxCompletionTokens = 400

const userLocationSystemPrompt = `You read the facts known about a user and say where they live and where they stay for a while. Reply with ONLY a JSON object: {"home":{"place":"...","country":"..","fact":"<id>"} or null,"current":{"place":"...","country":"..","since":"YYYY-MM-DD","until":"YYYY-MM-DD","fact":"<id>"} or null}.

- Use only what the facts state. Never guess a place from a language, a job, a team or a timezone.
- home: where the user lives. When facts disagree, the one seen last wins. place: the town or city as a map search finds it (no street, no district unless it is how the town is named). country: its ISO 3166-1 alpha-2 code only when the facts state the country, else "".
- current: only when a fact says the user stays somewhere else for a while (a trip, a stay, a temporary move), ongoing or planned, that has not ended before today. since: its first day; until: its last day, both reckoned from the fact's last_seen date ("this week" seen on a Tuesday runs from that Tuesday to Sunday, "on the 20th for a week" from the 20th to the 26th). Already begun with no start given: since is last_seen. No end given: until is 7 days after since.
- fact: the id of the fact the place comes from.
- No place known: null.`

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validDate(s string) bool {
	if !datePattern.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

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
			p.Since, p.Until = "", ""
			return p
		}
		if !validDate(p.Until) || (p.Since != "" && (!validDate(p.Since) || p.Since > p.Until)) {
			return nil
		}
		return p
	}
	loc.Home = clean(loc.Home, false)
	loc.Current = clean(loc.Current, true)
	return loc, true
}
