package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/sun"
	"github.com/trick77/loom/internal/weather"
)

// outdoorWindowsToolName answers "when can I go for a run / ride?". loom
// works the windows out itself (weather.Windows): a model reading two days of
// hours picks times after sunset and misreads the rain.
const outdoorWindowsToolName = "outdoor_windows"

// outdoorTimeout bounds one call: a lookup, the forecast and the air, each
// from the weather server's cache but its first per place and hour.
const outdoorTimeout = 20 * time.Second

// outdoorCategories are the turns the tool is offered in: "when can I go
// for a run?" reads as weather, training or fitness.
var outdoorCategories = []classifier.Category{classifier.Weather, classifier.Training, classifier.Health}

// Places tells where a user is today; nil leaves the outdoor windows to a
// place the user names.
type Places interface {
	UserPlace(ctx context.Context, user auth.User) (*location.Place, error)
}

// outdoorToolSpec is offered while the weather contract's forecast is there
// (as the start page asks it) for an account the weather server serves.
var outdoorToolSpec = toolSpec{
	name:    outdoorWindowsToolName,
	schema:  outdoorWindowsTool,
	offered: func(e *Engine, _ chat.Thread, gate toolGate) bool { return e.outdoorOn() && gate.offersOutdoor() },
	allowed: func(e *Engine, user auth.User) bool {
		return e.mcp.ToolAllowed(MCPPrincipal(user), weather.ForecastTool)
	},
	cites: func(ctx context.Context, t *Run, args map[string]any) (string, []mcp.Source) {
		return t.outdoorWindows(ctx, args)
	},
}

func (s *Engine) outdoorOn() bool {
	return s.mcp != nil && s.mcp.HasTool(weather.ForecastTool)
}

func (g toolGate) offersOutdoor() bool {
	if g.widenAll() {
		return true
	}
	for c := range g.activeCategories() {
		if slices.Contains(outdoorCategories, classifier.Normalize(c)) {
			return true
		}
	}
	return false
}

func outdoorWindowsTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: outdoorWindowsToolName,
			Description: "When to go out for a run or a bike ride today or tomorrow: the best windows by the forecast, best first, " +
				"each wholly in daylight (never before sunrise or after sunset, never already begun), dry ones before wet, then " +
				"sunnier, then milder and calmer, with sunrise, sunset and the time now at the place, and the air when it is worth " +
				"a word. Call it for every question about when to run, jog, ride or cycle; never work such a time out from the " +
				"forecast yourself and never propose a time outside the windows it returns. Without place it plans where the user " +
				"is; with place it plans there.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"activity": map[string]any{"type": "string", "enum": []string{string(weather.Run), string(weather.Ride)},
						"description": "run (also jog, walk) or ride (bike)."},
					"minutes": map[string]any{"type": "integer", "minimum": 15, "maximum": 480,
						"description": "How long, when the user says; by default a run 60, a ride 120."},
					"day": map[string]any{"type": "string", "enum": []string{"today", "tomorrow"}, "description": "Default today."},
					"place": map[string]any{"type": "string",
						"description": "A town or postal code the user names, e.g. Luzern; leave out for where the user is."},
					"country": map[string]any{"type": "string", "description": "ISO 3166-1 alpha-2 code of the place's country, e.g. CH."},
				},
				"required": []string{"activity"},
			},
		},
	}
}

// outdoorAnswer is what the model reads: the place, what was asked and the
// day's outlook.
type outdoorAnswer struct {
	Place    string `json:"place"`
	Activity string `json:"activity"`
	Minutes  int    `json:"minutes"`
	weather.Outlook
	Guide string `json:"guide"`
}

// outdoorGuide tells the model how to read the answer.
const outdoorGuide = "Times are the place's. Give the first window as the answer (from-to) with its reason in a sentence, " +
	"the others briefly. dry false on a window means no dry one is left: say it rains. none: dark (polar night), short " +
	"(daylight shorter than the activity), over (too little daylight left today: offer tomorrow), noForecast. Mention " +
	"air only when given."

// Bounds on the length the model may ask; one outside is clamped.
const (
	minOutdoorMinutes = 15
	maxOutdoorMinutes = 480
)

// outdoorWindows answers the tool, citing the weather's credit. Its weather
// calls carry the user (toolCallContext), as every MCP call of a turn does:
// a per-user weather server needs to know who asks.
func (t *Run) outdoorWindows(ctx context.Context, args map[string]any) (string, []mcp.Source) {
	activity := weather.Activity(strings.ToLower(strings.TrimSpace(stringArg(args, "activity"))))
	if activity != weather.Run && activity != weather.Ride {
		return "tool failed: activity must be run or ride", nil
	}
	minutes := activity.DefaultMinutes()
	if m, ok := args["minutes"].(float64); ok {
		minutes = int(min(max(m, minOutdoorMinutes), maxOutdoorMinutes))
	}
	ctx, cancel := context.WithTimeout(t.toolCallContext(ctx), outdoorTimeout)
	defer cancel()
	place, failure := t.outdoorPlace(ctx, stringArg(args, "place"), stringArg(args, "country"))
	if place == nil {
		return failure, nil
	}
	// The place's zone, else the user's: as the start page reads it.
	loc := weather.Zone(place.Timezone, t.user.Timezone)
	now := t.now().In(loc)
	day := now
	if strings.EqualFold(strings.TrimSpace(stringArg(args, "day")), "tomorrow") {
		day = time.Date(now.Year(), now.Month(), now.Day()+1, 12, 0, 0, 0, loc)
	}
	report, err := weather.GetWeather(ctx, t.e.mcp, place.Latitude, place.Longitude, loc.String(), 2)
	if err != nil {
		return "tool failed: " + err.Error(), nil
	}
	in := weather.WindowsInput{Forecast: report.Forecast, Air: report.Air, Day: day, Now: now, Loc: loc, Minutes: minutes,
		Activity: activity, Sun: sun.On(day, place.Latitude, place.Longitude, loc)}
	out := outdoorAnswer{Place: placeName(place), Activity: string(activity), Minutes: minutes, Outlook: weather.Windows(in),
		Guide: outdoorGuide}
	sources := report.Sources
	if out.Air != nil {
		sources = append(sources, report.AirSources...)
	}
	text, err := json.Marshal(out)
	if err != nil {
		return "tool failed: " + err.Error(), nil
	}
	return string(text), sources
}

// outdoorPlace is the place named, else where the user is today; nil with
// the reason when there is none.
func (t *Run) outdoorPlace(ctx context.Context, name, country string) (*location.Place, string) {
	if name = strings.TrimSpace(name); name != "" {
		p, err := weather.Find(ctx, t.e.mcp, name, strings.ToUpper(strings.TrimSpace(country)), weather.Language(t.user.ResponseLanguage))
		if err != nil {
			return nil, "tool failed: " + err.Error()
		}
		if p == nil {
			return nil, fmt.Sprintf("tool failed: no place called %q was found; ask the user which one they mean", name)
		}
		return p, ""
	}
	if t.e.places != nil {
		p, err := t.e.places.UserPlace(ctx, t.user)
		if err != nil {
			return nil, "tool failed: " + err.Error()
		}
		if p != nil {
			return p, ""
		}
	}
	return nil, "No place is known for the user: ask where, or call again with place."
}

func placeName(p *location.Place) string {
	parts := []string{p.Name}
	for _, s := range []string{p.Region, p.Country} {
		if s != "" && s != p.Name {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
