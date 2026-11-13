package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/toolkit/mcpserver"
)

// The tool name is the weather contract's, which loom-weather is held to.
func TestWeatherHistoryContract(t *testing.T) {
	if weatherHistoryTool != mcp.WeatherHistoryTool {
		t.Errorf("history tool %q, contract %q", weatherHistoryTool, mcp.WeatherHistoryTool)
	}
}

// The client against the real MCP handler loom-weather serves (stateless,
// no initialize): a call the handler stops taking fails here, not as a
// quiet "no temperature" in production.
func TestWeatherClient_mcpserver(t *testing.T) {
	var got struct {
		Latitude, Longitude float64
		Date                string
	}
	srv := httptest.NewServer(mcpserver.New(mcpserver.Server{Name: "weather", Version: "test", Tools: []mcpserver.Tool{{
		Name: mcp.WeatherHistoryTool,
		Schema: mcpserver.ObjectSchema(map[string]any{
			"latitude": map[string]any{"type": "number"}, "longitude": map[string]any{"type": "number"}, "date": map[string]any{"type": "string"},
		}),
		Run: func(_ context.Context, raw json.RawMessage) mcpserver.Result {
			if err := json.Unmarshal(raw, &got); err != nil {
				return mcpserver.ErrorResult(err.Error())
			}
			if got.Date == "1900-01-01" {
				return mcpserver.ErrorResult("history needs date as YYYY-MM-DD")
			}
			// loom-weather's History, attribution as a source like it.
			return mcpserver.Result{Text: `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich","date":"` + got.Date +
				`","hourly":[{"time":"` + got.Date + `T07:00","temperature":14.5,"humidity":80},{"time":"` + got.Date +
				`T08:00","temperature":16}]}`, Sources: []mcpserver.Source{{URL: "https://open-meteo.com/", Title: "Open-Meteo"}}}
		},
	}}}))
	t.Cleanup(srv.Close)
	w := &weatherClient{url: srv.URL, token: "t", http: srv.Client()}
	hours, err := w.history(t.Context(), 47.3812, 8.5412, "2026-03-01")
	if err != nil || len(hours) != 2 || hours["2026-03-01T08:00"] != 16 {
		t.Fatalf("history: %v %v", hours, err)
	}
	if got.Latitude != 47.3812 || got.Longitude != 8.5412 || got.Date != "2026-03-01" {
		t.Errorf("arguments sent: %+v", got)
	}
	if _, err := w.history(t.Context(), 47, 8, "1900-01-01"); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Errorf("tool error: %v", err)
	}
}

func TestFitHeat(t *testing.T) {
	// EF falls 1 % per °C from 1.5 at 15 °C.
	var ts, efs []float64
	for temp := 2.0; temp <= 30; temp += 2 {
		ts, efs = append(ts, temp), append(efs, 1.5*(1-0.01*(temp-15)))
	}
	f := fitHeat(ts, efs)
	if f.tooFew || f.tooNarrow || !within(f.efRef, 1.5, 1e-9) || !within(f.slope, -0.015, 1e-9) {
		t.Fatalf("fit %+v", f)
	}
	// At 25 °C (EF 1.35) a run at 150 bpm would have taken 150 × 1.35/1.5
	// = 135 bpm at 15 °C: +15.
	if v := f.bpmAt(25, 150); !within(v, 15, 1e-9) {
		t.Errorf("bpm at 25 °C %v, want 15", v)
	}
	// Data far from 15 °C: the reference moves into their span.
	var hot, hotEF []float64
	for temp := 22.0; temp <= 34; temp += 1.5 {
		hot, hotEF = append(hot, temp), append(hotEF, 1.5*(1-0.03*(temp-22)))
	}
	if g := fitHeat(hot, hotEF); g.ref != 22 || !within(g.efRef, 1.5, 1e-9) {
		t.Errorf("hot fit ref %v efRef %v", g.ref, g.efRef)
	}
	if !fitHeat(ts[:5], efs[:5]).tooFew || !fitHeat([]float64{10, 11, 12, 13, 14, 15, 16, 17}, efs[:8]).tooNarrow {
		t.Error("guards")
	}
}

// Two calls at once: each saves only what it looked up, never its older
// snapshot over the other's newer value.
func TestTempsSave_parallel(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	tl := &tools{svc: svc}
	set := func(c *temps, key string, v float64) {
		c.file.Temps[key], c.added[key] = &v, true
	}
	first := tl.tempsOf(userA, 1, 0)
	set(first, "1", 5)
	first.save()
	a, b := tl.tempsOf(userA, 1, 0), tl.tempsOf(userA, 1, 0)
	set(b, "1", 10)
	b.save()
	set(a, "2", 20)
	a.save()
	got := svc.store.loadTemps(userA)
	if got.Temps["1"] == nil || *got.Temps["1"] != 10 || got.Temps["2"] == nil || *got.Temps["2"] != 20 {
		t.Errorf("temps after two calls: %v %v", got.Temps["1"], got.Temps["2"])
	}
}

// fakeWeather is loom-weather's history tool: the day's temperature is 2 °C
// a month from January, every hour the same.
func fakeWeather(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer weather-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Params struct {
				Arguments struct {
					Date string `json:"date"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(b, &req)
		d, _ := time.Parse("2006-01-02", req.Params.Arguments.Date)
		temp := 2.0 * float64(d.Month())
		var hours []string
		for h := range 24 {
			hours = append(hours, fmt.Sprintf(`{"time":"%sT%02d:00","temperature":%g}`, req.Params.Arguments.Date, h, temp))
		}
		inner, _ := json.Marshal(fmt.Sprintf(`{"hourly":[%s]}`, strings.Join(hours, ",")))
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":%s}]}}`, inner)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// The watch records no temperature: the weather gives it, kept for good,
// and a year of runs gives the user's own heat effect.
func TestAnalysis_heat(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	// A run a month, the same heart rate, slower the warmer the month.
	for m := 1; m <= 10; m++ {
		f.addActivity(1, int64(7000+m), fmt.Sprintf("Run %d", m), "Run", local(2026, time.Month(m), 15, 7), nil, map[string]any{
			"distance": 10000.0, "moving_time": 3000 + m*40, "total_elevation_gain": 0.0, "average_heartrate": 145.0,
			"device_watts": false, "average_watts": 0.0, "weighted_average_watts": 0.0})
	}
	this := synthRun(2.5, 2.5)
	f.addActivity(1, 7100, "Hot run", "Run", local(2026, 10, 30, 7), &this, map[string]any{"distance": 7500.0, "moving_time": 3000,
		"average_heartrate": 146.0, "device_watts": false, "average_watts": 0.0, "weighted_average_watts": 0.0})
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	weather, calls := fakeWeather(t)
	svc.weather = &weatherClient{url: weather.URL, token: "weather-token", http: weather.Client()}

	r := callTool(t, h, userA, "activity_analysis", `{"id":7100}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{"heat (efficiency against temperature, 11 easy outdoor runs of the year, 2–20 °C, one source for all)",
		"this one at 20 °C (the weather at its start, its hours): about +"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if !strings.Contains(r.file("strava_7100_baseline.csv"), ",20.0\n") {
		t.Errorf("baseline temp_c:\n%s", r.file("strava_7100_baseline.csv"))
	}
	// Kept: a repeat and a restart ask the weather nothing.
	n := calls.Load()
	callTool(t, h, userA, "activity_analysis", `{"id":7100}`)
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	svc2.weather = svc.weather
	callTool(t, newServer(config{token: testToken}, svc2, "test"), userA, "activity_analysis", `{"id":7100}`)
	if calls.Load() != n {
		t.Errorf("kept temperatures read again: %d calls", calls.Load()-n)
	}

	// efficiency_trend shows the same kept temperatures, looking none up.
	n = calls.Load()
	if e := callTool(t, h, userA, "efficiency_trend", `{"sport":"running"}`); !strings.Contains(e.file("strava_efficiency.csv"), ",20,1\n") ||
		calls.Load() != n {
		t.Errorf("efficiency temp_c (%d weather calls):\n%s", calls.Load()-n, e.file("strava_efficiency.csv"))
	}
	// An indoor run gets no weather: Zwift's start point is an island.
	f.addActivity(1, 7300, "Zwift run", "VirtualRun", local(2026, 11, 1, 7), nil, map[string]any{"start_latlng": []float64{-11.6, 166.9}})
	if r := callTool(t, h, userA, "activity_analysis", `{"id":7300}`); !strings.Contains(r.Text, "heat: indoor, no air temperature") || calls.Load() != n {
		t.Errorf("indoor:\n%s", r.Text)
	}

	// A refresh (a corrected start, say) looks its temperature up again.
	n = calls.Load()
	if r := callTool(t, h, userA, "activity_analysis", `{"id":7100,"refresh":true}`); r.IsError || calls.Load() == n {
		t.Errorf("refresh kept the old temperature (%d weather calls): %.300s", calls.Load()-n, r.Text)
	}

	// No GPS start: no temperature for this one, so no lookup for the
	// others either (7250's would be new).
	f.addActivity(1, 7250, "Run with GPS", "Run", local(2026, 10, 31, 7), nil, map[string]any{"average_heartrate": 140.0})
	f.addActivity(1, 7260, "No GPS", "Run", local(2026, 11, 1, 9), nil, map[string]any{"start_latlng": []float64{}, "average_heartrate": 140.0})
	f.advance(10 * time.Minute)
	n = calls.Load()
	if r := callTool(t, h, userA, "activity_analysis", `{"id":7260}`); !strings.Contains(r.Text, "heat: no temperature for this activity") ||
		calls.Load() != n {
		t.Errorf("no GPS start (%d weather calls):\n%s", calls.Load()-n, r.Text)
	}

	// Without the weather: said, no fit.
	svc2.weather = nil
	f.addActivity(1, 7200, "New run", "Run", local(2026, 11, 2, 7), nil, map[string]any{"average_heartrate": 140.0})
	f.advance(10 * time.Minute)
	if r := callTool(t, newServer(config{token: testToken}, svc2, "test"), userA, "activity_analysis", `{"id":7200}`); !strings.Contains(r.Text,
		"heat: no temperature (the device records none and the weather service is not configured") {
		t.Errorf("no weather:\n%s", r.Text)
	}
}
