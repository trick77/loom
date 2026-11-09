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
				"sunnier, then milder and calmer (wind and gusts weigh more on a ride), with sunrise, sunset and the time now at " +
				"the place, the wind and the air when worth a word; or, for the week, its days ranked for going out. Call it for " +
				"every question about when or whether to run, jog, ride or cycle; never work such a time out from the forecast " +
				"yourself and never propose a time outside the windows it returns. Without place it plans where the user is; " +
				"with place it plans there.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"activity": map[string]any{"type": "string", "enum": []string{string(weather.Run), string(weather.Ride)},
						"description": "run (also jog, walk) or ride (bike)."},
					"minutes": map[string]any{"type": "integer", "minimum": 15, "maximum": 480,
						"description": "How long, when the user says; by default a run 60, a ride 120."},
					"day": map[string]any{"type": "string", "enum": []string{"today", "tomorrow", "week"},
						"description": "Default today; week ranks the next 7 days (which day, not what time)."},
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
// day's outlook, or the week's days.
type outdoorAnswer struct {
	Place    string `json:"place"`
	Activity string `json:"activity"`
	Minutes  int    `json:"minutes"`
	*weather.Outlook
	Week []weather.DayOutlook `json:"week,omitempty"`
	// Limits are the activity's wind thresholds (km/h).
	Limits weather.Limits `json:"windLimits"`
	Guide  string         `json:"guide"`
}

// outdoorGuide tells the model how to read the answer.
const outdoorGuide = "Times are the place's. Give the first window as the answer (from-to) with its reason in a sentence, " +
	"the others briefly. dry false on a window means no dry one is left: say it rains. wind windy: name it (windMax km/h " +
	"from windFrom, gusts); strong: warn, on a ride above all. none: dark (polar night), short (daylight shorter than the " +
	"activity), over (too little daylight left today: offer tomorrow), noForecast. week: days by rank (1 best; a day " +
	"with none ranks last, then strong wind, rain, wind, sun); name the best day or two and why; times can be planned " +
	"for today and tomorrow only. Mention air only when given."

// outdoorChartsGuide joins the guide when the chart files came along.
const outdoorChartsGuide = " The chart files: loomcharts.outdoor_day (the day asked: its hours, windows and wind " +
	"against the limits), outdoor_compare (the next two days with daylight), outdoor_week (the week); offer one when " +
	"it helps, draw it when asked."

// outdoorDays is how many days the forecast is asked: the week. The
// sidecar keeps 8, so the week costs no extra provider call.
const outdoorDays = 7

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
	report, err := weather.GetWeather(ctx, t.e.mcp, place.Latitude, place.Longitude, loc.String(), outdoorDays)
	if err != nil {
		return "tool failed: " + err.Error(), nil
	}
	plan := func(day time.Time) weather.Outlook {
		return weather.Windows(weather.WindowsInput{Forecast: report.Forecast, Air: report.Air, Day: day, Now: now, Loc: loc,
			Minutes: minutes, Activity: activity, Sun: sun.On(day, place.Latitude, place.Longitude, loc)})
	}
	day := strings.ToLower(strings.TrimSpace(stringArg(args, "day")))
	charts := t.takesToolFiles()
	// Both days are planned only when the week or the chart files need
	// them; the week reads them for today and tomorrow, outdoor_compare too.
	var outlooks []weather.Outlook
	switch {
	case charts || day == "week":
		outlooks = []weather.Outlook{plan(now), plan(time.Date(now.Year(), now.Month(), now.Day()+1, 12, 0, 0, 0, loc))}
	case day == "tomorrow":
		outlooks = []weather.Outlook{{}, plan(time.Date(now.Year(), now.Month(), now.Day()+1, 12, 0, 0, 0, loc))}
	default:
		outlooks = []weather.Outlook{plan(now)}
	}
	var week []weather.DayOutlook
	if charts || day == "week" {
		week = weather.Week(weather.WeekInput{Forecast: report.Forecast, Now: now, Loc: loc, Latitude: place.Latitude,
			Longitude: place.Longitude, Activity: activity, Minutes: minutes, Planned: outlooks})
	}
	guide := outdoorGuide
	if charts {
		guide += outdoorChartsGuide
	}
	out := outdoorAnswer{Place: placeName(place), Activity: string(activity), Minutes: minutes, Limits: activity.Limits(),
		Guide: guide}
	switch day {
	case "week":
		out.Week = week
	case "tomorrow":
		out.Outlook = &outlooks[1]
	default:
		out.Outlook = &outlooks[0]
	}
	sources := report.Sources
	if out.Outlook != nil && out.Air != nil {
		sources = append(sources, report.AirSources...)
	}
	text, err := json.Marshal(out)
	if err != nil {
		return "tool failed: " + err.Error(), nil
	}
	if !charts {
		return string(text), sources
	}
	asked := outlooks[0].Date
	if out.Outlook != nil {
		asked = out.Date
	}
	files := weather.ChartFiles(weather.ChartInput{Forecast: report.Forecast, Loc: loc, Activity: activity,
		Minutes: minutes, Date: asked, Outlooks: outlooks, Week: week})
	return string(text) + t.keepToolFiles(files), sources
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
