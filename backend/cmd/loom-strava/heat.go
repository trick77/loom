package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Heat: how much warmer days lift the user's heart rate for the same
// speed or power, from their own activities, and what that means for this
// one. Most watches record no temperature (the user's does not), so an
// activity without one gets the weather of its start point and hours from
// loom-weather's history tool, kept per activity: the past does not change.
// loom-weather is reached over MCP like loom reaches it.
const (
	// weatherHistoryTool is the weather contract's history tool
	// (mcp.WeatherHistoryTool, held to it by a test).
	weatherHistoryTool = "history"
	weatherTimeout     = 8 * time.Second
	// maxWeatherLookups and weatherBudget bound one analysis's history
	// calls in number and time: the heat comes unasked with every analysis
	// and must not stall it; a later call fills the rest (kept).
	maxWeatherLookups = 12
	weatherBudget     = 12 * time.Second
	// weatherRecentDays: the weather answers this recent a day from the
	// forecast model's past hours, older ones from the archive's
	// reanalysis. A recent day's value is kept only until the archive has
	// it (a few days on), then read again: the fit holds one model.
	weatherRecentDays = 80
	archiveLagDays    = 7
	// minHeatN and minHeatSpanC: fewer activities, or a narrower spread of
	// temperatures, and the fit is the noise of a few days.
	minHeatN     = 8
	minHeatSpanC = 8.0
	// heatRefC is the temperature the activity is read against, moved into
	// the fit's own span when the data lie elsewhere (an extrapolation far
	// out made the reference efficiency zero or negative).
	heatRefC = 15.0
)

type weatherClient struct {
	url, token string
	http       *http.Client
}

// history is the hourly temperature (°C) of a past local day at a point:
// "YYYY-MM-DDTHH:MM" → °C.
func (w *weatherClient) history(ctx context.Context, lat, lon float64, date string) (map[string]float64, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": weatherHistoryTool, "arguments": map[string]any{"latitude": lat, "longitude": lon, "date": date}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weather answered %d", resp.StatusCode)
	}
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Result.Content) == 0 {
		return nil, fmt.Errorf("weather answer unreadable")
	}
	if r.Result.IsError {
		return nil, errors.New("weather: " + r.Result.Content[0].Text)
	}
	var h struct {
		Hourly []struct {
			Time        string  `json:"time"`
			Temperature float64 `json:"temperature"`
		} `json:"hourly"`
	}
	if err := json.Unmarshal([]byte(r.Result.Content[0].Text), &h); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, x := range h.Hourly {
		out[x.Time] = x.Temperature
	}
	return out, nil
}

// tempsFile is each activity's temperature from the weather, kept per user
// next to the metrics; a null is "looked up, none to be had". Recent marks
// a value from the forecast model's past hours, read again once the
// archive has the day.
type tempsFile struct {
	Athlete int64               `json:"athlete,omitempty"`
	Temps   map[string]*float64 `json:"temps"`
	Recent  map[string]bool     `json:"recent,omitempty"`
}

const tempsSuffix = ".temps.json"

func (s fileStore) loadTemps(user string) tempsFile {
	var f tempsFile
	if !s.loadSide(user, tempsSuffix, &f) || f.Temps == nil {
		return tempsFile{Temps: map[string]*float64{}, Recent: map[string]bool{}}
	}
	if f.Recent == nil {
		f.Recent = map[string]bool{}
	}
	return f
}

// saveTemps keeps the newest maxStoredActivities (Strava's ids grow with
// time: the lowest go).
func (s fileStore) saveTemps(user string, f tempsFile) error {
	if over := len(f.Temps) - maxStoredActivities; over > 0 {
		ids := make([]int64, 0, len(f.Temps))
		for k := range f.Temps {
			id, _ := strconv.ParseInt(k, 10, 64)
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids[:over] {
			k := strconv.FormatInt(id, 10)
			delete(f.Temps, k)
			delete(f.Recent, k)
		}
	}
	return s.saveSide(user, tempsSuffix, f)
}

// temps is a call's view of the user's temperatures: with the weather
// configured, the weather's for every activity (kept, or looked up within
// the budget), never mixed with a device's (a wrist or a bike unit in the
// sun reads degrees above the air, and a fit across both sources slopes on
// the jump); without it, the device's. save writes what it added.
type temps struct {
	t        *tools
	user     string
	athlete  int64
	file     tempsFile
	added    map[string]bool // keys looked up this call: only these are saved
	budget   int
	deadline time.Time      // no lookup past it
	failed   bool           // a lookup failed: none more this call
	skipped  map[int64]bool // wanted a lookup the call could not make
}

func (t *tools) tempsOf(user string, athleteID int64, budget int) *temps {
	sl := t.svc.slot(user)
	sl.dataMu.Lock()
	f := t.svc.store.loadTemps(user)
	sl.dataMu.Unlock()
	if otherAthlete(f.Athlete, athleteID) {
		f = tempsFile{Temps: map[string]*float64{}, Recent: map[string]bool{}}
	}
	return &temps{t: t, user: user, athlete: athleteID, file: f, budget: budget, deadline: time.Now().Add(weatherBudget),
		skipped: map[int64]bool{}, added: map[string]bool{}}
}

// kept is x's kept weather temperature: ok false when none is kept or a
// recent one is due for its archive value.
func (c *temps) kept(x summaryActivity) (float64, bool) {
	key := strconv.FormatInt(x.ID, 10)
	v, ok := c.file.Temps[key]
	if !ok {
		return nan, false
	}
	if c.file.Recent[key] && x.localStart().Before(c.t.svc.now().AddDate(0, 0, -(weatherRecentDays+archiveLagDays))) {
		return nan, false
	}
	if v == nil {
		return nan, true
	}
	return *v, true
}

// shown is x's temperature for a table, no lookup: the kept weather one,
// else the device's.
func (c *temps) shown(x summaryActivity) float64 {
	if x.indoor() {
		return nan
	}
	if v, ok := c.kept(x); ok && finite(v) {
		return v
	}
	if x.AverageTemp != 0 {
		return x.AverageTemp
	}
	return nan
}

// of is x's temperature (°C) for the fit and where from ("weather",
// "device"); NaN, "" without. Indoor has no air temperature (Zwift's start
// point is a made-up island).
func (c *temps) of(ctx context.Context, x summaryActivity) (float64, string) {
	if x.indoor() {
		return nan, ""
	}
	w := c.t.svc.weather
	if w == nil {
		// Strava's summary cannot tell 0 °C from no sensor: 0 counts as none.
		if x.AverageTemp != 0 {
			return x.AverageTemp, "device"
		}
		return nan, ""
	}
	if v, ok := c.kept(x); ok {
		if !finite(v) {
			return nan, ""
		}
		return v, "weather"
	}
	if !x.StartLatLng.ok() {
		return nan, ""
	}
	// No lookup past the budget, after a failure (a hung or older weather
	// sidecar would cost every lookup's timeout), or near a deadline.
	if c.budget <= 0 || c.failed || time.Now().After(c.deadline) {
		c.skipped[x.ID] = true
		return nan, ""
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < minTimeLeft+weatherTimeout {
		c.skipped[x.ID] = true
		return nan, ""
	}
	// The mean of the activity's hours, from its start hour to its end's;
	// past local midnight the next day's too. The budget counts calls, not
	// activities: an overnight one costs two.
	start := x.localStart()
	end := start.Add(time.Duration(max(x.ElapsedTime, 1)) * time.Second)
	hours := map[string]float64{}
	for day := dayOf(start); !day.After(end); day = day.AddDate(0, 0, 1) {
		if c.budget <= 0 || time.Now().After(c.deadline) {
			c.skipped[x.ID] = true
			return nan, ""
		}
		c.budget--
		got, err := w.history(ctx, x.StartLatLng[0], x.StartLatLng[1], day.Format("2006-01-02"))
		if err != nil {
			// Not kept: a later call asks again.
			slog.Warn("strava: weather history failed", "err", err)
			c.failed = true
			c.skipped[x.ID] = true
			return nan, ""
		}
		for k, v := range got {
			hours[k] = v
		}
	}
	sum, n := 0.0, 0
	for h := start.Truncate(time.Hour); !h.After(end); h = h.Add(time.Hour) {
		if v, ok := hours[h.Format("2006-01-02T15:04")]; ok {
			sum, n = sum+v, n+1
		}
	}
	key := strconv.FormatInt(x.ID, 10)
	c.added[key] = true
	c.file.Recent[key] = !start.Before(c.t.svc.now().AddDate(0, 0, -weatherRecentDays))
	if n == 0 {
		c.file.Temps[key] = nil
		return nan, ""
	}
	v := math.Round(sum/float64(n)*10) / 10
	c.file.Temps[key] = &v
	return v, "weather"
}

// save writes this call's lookups, unless the user disconnected or
// reconnected to another athlete meanwhile. Only these: the rest of the
// call's snapshot may be older than what a parallel call saved.
func (c *temps) save() {
	if len(c.added) == 0 {
		return
	}
	sl := c.t.svc.slot(c.user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if !c.t.svc.stillConnected(c.user, c.athlete) {
		return
	}
	f := c.t.svc.store.loadTemps(c.user)
	if otherAthlete(f.Athlete, c.athlete) {
		f = tempsFile{Temps: map[string]*float64{}, Recent: map[string]bool{}}
	}
	f.Athlete = c.athlete
	for k := range c.added {
		f.Temps[k] = c.file.Temps[k]
		f.Recent[k] = c.file.Recent[k]
		if !f.Recent[k] {
			delete(f.Recent, k)
		}
	}
	if err := c.t.svc.store.saveTemps(c.user, f); err != nil {
		slog.Error("strava: saving temperatures failed", "err", err)
	}
}

// heatFit is the efficiency against temperature over the user's activities
// of one sport: slope per °C relative to the efficiency at ref.
type heatFit struct {
	n                 int
	lo, hi            float64 // the temperatures the fit spans
	ref               float64 // heatRefC, moved into lo..hi
	efRef, slope      float64 // EF at ref, EF per °C
	tooFew, tooNarrow bool
}

func fitHeat(ts, efs []float64) heatFit {
	f := heatFit{n: len(ts), lo: math.Inf(1), hi: math.Inf(-1), efRef: nan, slope: nan, ref: heatRefC}
	for _, t := range ts {
		f.lo, f.hi = math.Min(f.lo, t), math.Max(f.hi, t)
	}
	if f.n < minHeatN {
		f.tooFew = true
		return f
	}
	if f.hi-f.lo < minHeatSpanC {
		f.tooNarrow = true
		return f
	}
	f.ref = math.Min(math.Max(heatRefC, f.lo), f.hi)
	mt, me := mean(ts), mean(efs)
	var num, den float64
	for i := range ts {
		num += (ts[i] - mt) * (efs[i] - me)
		den += (ts[i] - mt) * (ts[i] - mt)
	}
	f.slope = num / den
	f.efRef = me + f.slope*(f.ref-mt)
	return f
}

// bpmAt is how many beats more (less) an activity at temp with average
// heart rate hr ran than it would have at ref for the same output: the
// output is EF(temp) × hr, at ref it takes that over EF(ref).
func (f heatFit) bpmAt(temp, hr float64) float64 {
	ef := f.efRef + f.slope*(temp-f.ref)
	if !finite(ef) || ef <= 0 || f.efRef <= 0 {
		return nan
	}
	return hr * (1 - ef/f.efRef)
}
