package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/weather"
)

type fakePlaces struct {
	place *location.Place
	err   error
}

func (f fakePlaces) UserPlace(context.Context, auth.User) (*location.Place, error) {
	return f.place, f.err
}

var home = &location.Place{Name: "Baden", Region: "Aargau", Country: "Schweiz", Latitude: 47.47, Longitude: 8.3, Timezone: "Europe/Zurich"}

// weatherServer answers the weather tools: two dry, grey days in Zurich from
// 2026-11-09 00:00, Luzern for a lookup, and records what it was asked.
type weatherServer struct {
	mu      sync.Mutex
	asked   []string
	args    []map[string]any
	callers []string
	air     bool
}

func (w *weatherServer) service() fakeMCPService {
	available := map[string]bool{weather.ForecastTool: true, weather.GeocodeTool: true}
	if w.air {
		available[weather.AirQualityTool] = true
	}
	return fakeMCPService{
		Available: available,
		Sources:   []mcp.Source{{URL: "https://weather.example/credit", Title: "credit"}},
		CallFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if name == weather.AirQualityTool {
				// Asked alongside the forecast: kept out of the order tests read.
				return `{"hourly":[],"daily":[{"date":"2026-11-09","pollen":{"birch":200}}]}`, nil
			}
			w.asked, w.args = append(w.asked, name), append(w.args, args)
			if p, ok := mcp.PrincipalFrom(ctx); ok && name == weather.ForecastTool {
				w.callers = append(w.callers, p.ID)
			}
			switch name {
			case weather.GeocodeTool:
				if args["query"] == "Nowhere" {
					return `{"places":[]}`, nil
				}
				return `{"places":[{"name":"Luzern","region":"Luzern","country":"Schweiz","latitude":47.05,"longitude":8.31,"timezone":"Europe/Zurich"}]}`, nil
			case weather.ForecastTool:
				var hours []string
				for i := range 48 {
					t := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
					hours = append(hours, fmt.Sprintf(`{"time":%q,"temperature":%d,"code":3,"precipitationProbability":0,"sunshineMinutes":0}`,
						t.Format("2006-01-02T15:04"), 4+i%24/3))
				}
				return `{"hourly":[` + strings.Join(hours, ",") + `],"daily":[]}`, nil
			}
			return "", errors.New("unknown tool")
		},
	}
}

func outdoorRun(e *Engine, at time.Time) *Run {
	r := offeredRun(e, chat.Thread{ID: "t1"}, newToolGate(string(classifier.Weather), "", ""))
	r.start, r.timezone = at, "Europe/Zurich"
	return r
}

func callOutdoor(t *testing.T, r *Run, args string, reg *webSourceRegistry) string {
	t.Helper()
	out, _, handled := r.executeBuiltInTool(context.Background(),
		llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: outdoorWindowsToolName, Arguments: args}}, reg)

	if !handled {
		t.Fatal("outdoor_windows not handled")
	}
	return out
}

func answerOf(t *testing.T, out string) outdoorAnswer {
	t.Helper()
	_, body, _ := strings.Cut(out, "{")
	var a outdoorAnswer
	if err := json.Unmarshal([]byte("{"+body), &a); err != nil {
		t.Fatalf("not an answer: %v\n%s", err, out)
	}
	return a
}

var morning = time.Date(2026, 11, 9, 8, 0, 0, 0, mustLoad("Europe/Zurich"))

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// The tool comes with the weather, on weather, training and fitness turns.
func TestOutdoorWindowsOffered(t *testing.T) {
	w := &weatherServer{}
	on := &Engine{mcp: w.service()}
	off := &Engine{mcp: fakeMCPService{}}
	for _, tc := range []struct {
		e        *Engine
		category string
		want     bool
	}{
		{on, string(classifier.Weather), true},
		{on, string(classifier.Training), true},
		{on, string(classifier.Health), true},
		{on, string(classifier.Coding), false},
		{on, "", true},
		{off, string(classifier.Weather), false},
		{&Engine{}, string(classifier.Weather), false},
	} {
		got := false
		for _, tool := range tc.e.availableTools(chat.Thread{}, newToolGate(tc.category, "", "")) {
			got = got || tool.Function.Name == outdoorWindowsToolName
		}
		if got != tc.want {
			t.Errorf("%q, weather %v: offered %v, want %v", tc.category, tc.e.mcp != nil, got, tc.want)
		}
	}
	// A turn that drifts to the weather gets it too.
	drift := newToolGate(string(classifier.Coding), string(classifier.Weather), "")
	if !drift.offersOutdoor() {
		t.Fatal("not offered on a drift to the weather")
	}
}

// Without a place it plans where the user is, and cites the weather's credit.
func TestOutdoorWindowsAtHome(t *testing.T) {
	w := &weatherServer{}
	r := outdoorRun(&Engine{mcp: w.service(), places: fakePlaces{place: home}}, morning)
	out := callOutdoor(t, r, `{"activity":"ride"}`, newWebSourceRegistryAfter(0))
	if !strings.HasPrefix(out, "Source [1]: credit") {
		t.Fatalf("credit not cited: %s", out)
	}
	a := answerOf(t, out)
	if a.Place != "Baden, Aargau, Schweiz" || a.Activity != "ride" || a.Minutes != 120 || a.Date != "2026-11-09" ||
		a.Now != "08:00" || len(a.Windows) == 0 || a.Sunset == "" || a.Guide == "" {
		t.Fatalf("answer = %+v", a)
	}
	for _, win := range a.Windows {
		if win.To > a.Sunset {
			t.Fatalf("window past sunset: %+v", a)
		}
	}
	if len(w.asked) != 1 || w.args[0]["latitude"] != 47.47 || w.args[0]["timezone"] != "Europe/Zurich" {
		t.Fatalf("asked %v %v", w.asked, w.args)
	}
	// The weather is asked as the user, as every MCP call of a turn is.
	if len(w.callers) != 1 || w.callers[0] != testUser.ID {
		t.Fatalf("asked as %v", w.callers)
	}
}

// A length out of bounds is clamped, never swapped for the default.
func TestOutdoorWindowsClampsMinutes(t *testing.T) {
	w := &weatherServer{}
	r := outdoorRun(&Engine{mcp: w.service(), places: fakePlaces{place: home}}, morning)
	for args, want := range map[string]int{`{"activity":"run","minutes":10}`: 15, `{"activity":"ride","minutes":600}`: 480} {
		if a := answerOf(t, callOutdoor(t, r, args, nil)); a.Minutes != want {
			t.Errorf("%s: minutes %d, want %d", args, a.Minutes, want)
		}
	}
}

// "Wann kann ich heute in Luzern joggen?": a named place is looked up.
func TestOutdoorWindowsElsewhere(t *testing.T) {
	w := &weatherServer{}
	r := outdoorRun(&Engine{mcp: w.service(), places: fakePlaces{place: home}}, morning)
	a := answerOf(t, callOutdoor(t, r, `{"activity":"run","place":"Luzern","country":"ch","minutes":45}`, nil))
	if a.Place != "Luzern, Schweiz" || a.Minutes != 45 {
		t.Fatalf("answer = %+v", a)
	}
	if w.asked[0] != weather.GeocodeTool || w.args[0]["country"] != "CH" || w.args[0]["language"] != "en" || w.args[1]["latitude"] != 47.05 {
		t.Fatalf("asked %v %v", w.asked, w.args)
	}
	if out := callOutdoor(t, r, `{"activity":"run","place":"Nowhere"}`, nil); !strings.Contains(out, "no place called") {
		t.Fatalf("unknown place = %s", out)
	}
}

func TestOutdoorWindowsTomorrowAndAir(t *testing.T) {
	w := &weatherServer{air: true}
	r := outdoorRun(&Engine{mcp: w.service(), places: fakePlaces{place: home}}, morning.Add(12*time.Hour))
	a := answerOf(t, callOutdoor(t, r, `{"activity":"Run","day":"Tomorrow"}`, nil))
	if a.Date != "2026-11-10" || a.Now != "" || len(a.Windows) == 0 {
		t.Fatalf("tomorrow = %+v", a)
	}
	// Tonight is over for a run; the air is today's.
	today := answerOf(t, callOutdoor(t, r, `{"activity":"run"}`, nil))
	if today.None != weather.NoneOver || len(today.Windows) != 0 {
		t.Fatalf("tonight = %+v", today)
	}
	r = outdoorRun(&Engine{mcp: w.service(), places: fakePlaces{place: home}}, morning)
	if a := answerOf(t, callOutdoor(t, r, `{"activity":"run"}`, nil)); a.Air == nil || a.Air.Pollen["birch"] != "high" {
		t.Fatalf("air = %+v", a.Air)
	}
}

func TestOutdoorWindowsFailures(t *testing.T) {
	w := &weatherServer{}
	for name, tc := range map[string]struct {
		places Places
		args   string
		want   string
	}{
		"no place":      {nil, `{"activity":"run"}`, "No place is known"},
		"place unknown": {fakePlaces{}, `{"activity":"run"}`, "No place is known"},
		"place failed":  {fakePlaces{err: errors.New("db down")}, `{"activity":"run"}`, "tool failed: db down"},
		"bad activity":  {fakePlaces{place: home}, `{"activity":"swim"}`, "activity must be run or ride"},
		"bad arguments": {fakePlaces{place: home}, `{nope`, "invalid arguments"},
	} {
		r := outdoorRun(&Engine{mcp: w.service(), places: tc.places}, morning)
		if out := callOutdoor(t, r, tc.args, nil); !strings.Contains(out, tc.want) {
			t.Errorf("%s: %s", name, out)
		}
	}
	// An account the weather server is closed to is never offered the tool.
	closed := w.service()
	closed.Users = map[string][]string{mcp.WeatherServer: {"someone_else"}}
	r := outdoorRun(&Engine{mcp: closed, places: fakePlaces{place: home}}, morning)
	if r.offered[outdoorWindowsToolName] != nil {
		t.Fatal("offered to an account the weather is closed to")
	}
	// A dead weather server fails the call.
	dead := w.service()
	dead.CallFunc = func(context.Context, string, map[string]any) (string, error) { return "", errors.New("down") }
	r = outdoorRun(&Engine{mcp: dead, places: fakePlaces{place: home}}, morning)
	if out := callOutdoor(t, r, `{"activity":"run"}`, nil); !strings.HasPrefix(out, "tool failed") {
		t.Fatalf("dead = %s", out)
	}
}
