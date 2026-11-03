package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hrPt(id int64, max10, best20, w20 float64) hrPoint {
	v := map[string]float64{}
	if max10 > 0 {
		v["hr_max10"] = max10
	}
	if best20 > 0 {
		v["hr_20m"] = best20
	}
	if w20 > 0 {
		v["hr_20m_w"] = w20
	}
	return hrPoint{ref: hrRef{ID: id}, vals: v}
}

func TestEstimateHR(t *testing.T) {
	// 195 is a lone strap spike: nothing within 5 bpm. 183 is confirmed by 181.
	e := estimateHR([]hrPoint{hrPt(1, 195, 150, 0), hrPt(2, 183, 172, 260), hrPt(3, 181, 165, 0), hrPt(4, 170, 0, 0)})
	if e.MaxHR != 183 || e.MaxAt.ID != 2 || e.ConfirmAt.ID != 3 {
		t.Errorf("max = %v at %d confirmed by %d", e.MaxHR, e.MaxAt.ID, e.ConfirmAt.ID)
	}
	if e.Best20 != 172 || e.Best20At.ID != 2 || e.Best20W != 260 {
		t.Errorf("best 20 min = %v at %d, %v W", e.Best20, e.Best20At.ID, e.Best20W)
	}
	near(t, "LTHR", e.LTHR, 0.95*172, 1e-9)

	// Every top value alone, one activity, none: no max; no 20 min: no LTHR.
	for _, pts := range [][]hrPoint{{hrPt(1, 190, 0, 0), hrPt(2, 180, 0, 0)}, {hrPt(1, 180, 0, 0)}, nil} {
		e := estimateHR(pts)
		if !math.IsNaN(e.MaxHR) || !math.IsNaN(e.LTHR) || !math.IsNaN(e.Best20W) {
			t.Errorf("estimate of %d points = %+v", len(pts), e)
		}
	}
}

func TestHRFamilyZones(t *testing.T) {
	cyc, run := hrFamilies[0], hrFamilies[1]
	if got := zoneList(cyc.zones(170), ""); got != "Z1 0–137 · Z2 138–152 · Z3 153–159 · Z4 160–169 · Z5 170+" {
		t.Errorf("cycling zones = %s", got)
	}
	if got := zoneList(run.zones(170), ""); got != "Z1 0–144 · Z2 145–152 · Z3 153–161 · Z4 162–169 · Z5 170+" {
		t.Errorf("running zones = %s", got)
	}
	for sport, want := range map[string]string{"Ride": "cycling", "GravelRide": "cycling", "MountainBikeRide": "cycling",
		"VirtualRide": "cycling", "Run": "running", "TrailRun": "running", "VirtualRun": "running", "EBikeRide": "", "Walk": "", "Hike": ""} {
		got := ""
		if f, ok := familyOf(sport); ok {
			got = f.key
		}
		if got != want {
			t.Errorf("familyOf(%s) = %q, want %q", sport, got, want)
		}
	}
}

// hrFixture: Anna (FTP 250, Strava's default zones) with four cycling
// activities (a gravel ride among them), two runs, an e-bike ride and a ride
// older than a year; Ben (athlete 2) with one ride of his own.
func hrFixture(t *testing.T) (*fakeStrava, *service, string) {
	t.Helper()
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	f.addAthlete(2, "Ben", "code-b", 300)
	f.zones[1].(map[string]any)["heart_rate"].(map[string]any)["custom_zones"] = false
	add := func(athlete, id int64, name, sport string, day int, hrOff float64) {
		st := synthRideWith(1, 1, hrOff, false)
		f.addActivity(athlete, id, name, sport, local(2026, 10, day, 9), &st, map[string]any{"max_heartrate": 170 + hrOff})
	}
	add(1, 3001, "Spike ride", "Ride", 1, 10)  // 180: alone at the top
	add(1, 3002, "Gravel", "GravelRide", 2, 4) // 174: the max, confirmed by 172
	add(1, 3003, "Steady", "Ride", 3, 2)       // 172
	add(1, 3004, "Zwift", "VirtualRide", 4, 0) // 170
	add(1, 3005, "Trail", "TrailRun", 5, 8)    // 178
	add(1, 3006, "Run", "Run", 6, 9)           // 179
	add(1, 3007, "E-bike", "EBikeRide", 7, 30)
	old := synthRideWith(1, 1, 40, false)
	f.addActivity(1, 3008, "Last year", "Ride", local(2025, 10, 1, 9), &old, nil)
	add(2, 4001, "Ben's ride", "Ride", 8, 20)
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	connect(t, h, userB, "code-b")
	return f, svc, dir
}

func TestHRZones(t *testing.T) {
	f, svc, dir := hrFixture(t)
	h := newServer(config{token: testToken}, svc, "test")

	// An entry stored before the HR keys existed is read again; one of an
	// activity outside the window stays as it was.
	if err := svc.store.saveMetrics(userA, metricFile{Athlete: 1, Activities: map[string]storedMetrics{
		"3003": {V: metricsVersion, Start: "2026-10-03T09:00:00", MovingS: 1, Vals: map[string]float64{"np": 1}},
		"9999": {V: metricsVersion, Start: "2020-01-01T09:00:00", MovingS: 1, Vals: map[string]float64{"np": 1}},
		// Read by 0.6.0: HR keys, no sport or name; those come from the list.
		"3004": {V: metricsVersion, Rev: entryRev, Start: "2026-10-04T09:00:00", MovingS: 1, Vals: map[string]float64{"hr_max10": 170, "np": 2}},
	}}); err != nil {
		t.Fatal(err)
	}
	r := callTool(t, h, userA, "hr_zones", `{}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	s := buildSeries(synthRideWith(1, 1, 10, false))
	lthr := 0.95 * computeMetrics(s, 0, s.n, nil, nil).HR20m
	for _, want := range []string{
		"2025-11-03 – 2026-11-03",
		"cycling (Ride, GravelRide, MountainBikeRide, VirtualRide): 4 with heart rate, 4 read",
		"max heart rate: 174 bpm, best 10 s in [1] 2026-10-02 · Gravel; [2] 2026-10-03 · Steady within 5 bpm (172 bpm)",
		fmt.Sprintf("threshold heart rate (LTHR): %.0f bpm", lthr),
		"in [3] 2026-10-01 · Spike ride",
		"96 % of FTP: a threshold effort",
		"zones: " + zoneList(hrFamilies[0].zones(lthr), " bpm"),
		"running (Run, TrailRun, VirtualRun): 2 with heart rate, 2 read",
		"max heart rate: 179 bpm",
		"Strava's heart-rate zones (Strava's defaults, usually age-based): Z1 0–123 bpm",
		"Custom zones set in Strava replace both",
		"calculated 2026-11-03 10:02 UTC and stored",
	} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("hr_zones lacks %q:\n%s", want, r.Text)
		}
	}
	if len(r.Sources) != 5 || r.Sources[0]["url"] != "https://www.strava.com/activities/3002" {
		t.Errorf("sources = %v", r.Sources)
	}
	for _, id := range []string{"3007", "3008", "4001"} {
		if n := f.count("/activities/" + id + "/streams"); n != 0 {
			t.Errorf("streams of %s read %d times", id, n)
		}
	}
	m, _ := svc.store.loadMetrics(userA)
	if v := m.Activities["3003"].Vals; v["hr_max10"] != 172 || v["np"] <= 1 {
		t.Errorf("old entry not read again: %v", v)
	}
	if v := m.Activities["9999"].Vals; v["np"] != 1 || len(v) != 1 {
		t.Errorf("entry outside the window changed: %v", v)
	}
	if e := m.Activities["3004"]; e.Sport != "VirtualRide" || e.Name != "Zwift" || e.Vals["np"] != 2 || f.count("/activities/3004/streams") != 0 {
		t.Errorf("sport and name not filled in from the list, or streams read again: %+v", e)
	}
	// The zones are stored with the date they were calculated.
	cyc, run := m.HRZones["cycling"], m.HRZones["running"]
	if !cyc.CalculatedAt.Equal(f.clock()) || cyc.MaxHR != 174 || cyc.MaxAt == nil || cyc.MaxAt.ID != 3002 || cyc.WithHR != 4 || cyc.Used != 4 ||
		math.Abs(cyc.LTHR-lthr) > 0.05 || zoneList(cyc.Zones, "") != zoneList(hrFamilies[0].zones(lthr), "") || cyc.Best20W <= 0 || cyc.From != "2025-11-03" {
		t.Errorf("stored cycling zones: %+v", cyc)
	}
	if run.MaxHR != 179 || run.Best20At == nil || run.Best20At.Name != "Run" || len(run.Zones) != 5 {
		t.Errorf("stored running zones: %+v", run)
	}

	// Everything stored: a second call reads no stream.
	before := f.count("/activities/3001/streams")
	if r := callTool(t, h, userA, "hr_zones", `{"sport":"running"}`); r.IsError || strings.Contains(r.Text, "cycling") || !strings.Contains(r.Text, "179 bpm") {
		t.Errorf("running only: %s", r.Text)
	}
	if f.count("/activities/3001/streams") != before {
		t.Error("stored HR values not reused")
	}
	if r := callTool(t, h, userA, "hr_zones", `{"sport":"swimming"}`); !r.IsError {
		t.Errorf("unknown sport: %s", r.Text)
	}
	// Strava's zones are the defaults: time in zones and the profile use the
	// stored ones; the e-bike has none and keeps Strava's, labelled.
	cyc = loadZones(t, svc)["cycling"]
	wantNote := fmt.Sprintf("the user's own zones from their streams (cycling, LTHR %s, calculated 2026-11-03), not Strava's age-based defaults", bpm(cyc.LTHR))
	ra := callTool(t, h, userA, "activity_analysis", `{"id":3002}`)
	if !strings.Contains(ra.Text, wantNote) || strings.Contains(ra.Text, "HR zone times from Strava") {
		t.Errorf("analysis zone note:\n%s", ra.Text)
	}
	if !strings.Contains(ra.file("strava_3002_streams.csv"), ",5\n") {
		t.Error("the streams CSV does not bucket with the user's zones (the climb is their Z5)")
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":3007}`); !strings.Contains(r.Text, "Strava's default zones, usually age-based)") {
		t.Errorf("e-bike zone note:\n%s", r.Text)
	}
	rp := callTool(t, h, userA, "profile", `{}`)
	if !strings.Contains(rp.Text, "heart-rate zones (Strava's defaults, usually age-based; the user's own from their streams") ||
		!strings.Contains(rp.Text, "heart-rate zones from the user's own streams (cycling, calculated 2026-11-03, max 174 bpm, LTHR") ||
		!strings.Contains(rp.Text, "(running, calculated 2026-11-03, max 179 bpm") {
		t.Errorf("profile zones:\n%s", rp.Text)
	}

	// Per user: Ben sees his own ride only, never Anna's values or file.
	rb := callTool(t, h, userB, "hr_zones", `{}`)
	if rb.IsError || !strings.Contains(rb.Text, "1 with heart rate, 1 read") || strings.Contains(rb.Text, "· Gravel") ||
		!strings.Contains(rb.Text, "max heart rate: needs") || !strings.Contains(rb.Text, "below threshold, so the LTHR is likely low") || !strings.Contains(rb.Text, "Strava's heart-rate zones (custom)") {
		t.Errorf("Ben:\n%s", rb.Text)
	}
	mb, _ := svc.store.loadMetrics(userB)
	if _, ok := mb.Activities["4001"]; !ok || len(mb.Activities) != 1 {
		t.Errorf("Ben's file: %v", mb.Activities)
	}
	if _, err := os.Stat(filepath.Join(dir, metricsDir, userB+".json")); err != nil {
		t.Error(err)
	}
	// Ben's stored zones are his own; his custom Strava zones still win.
	if z := mb.HRZones["cycling"]; z.LTHR <= 0 || z.MaxAt != nil || z.Best20At.ID != 4001 {
		t.Errorf("Ben's stored zones: %+v", z)
	}
	if r := callTool(t, h, userB, "profile", `{}`); strings.Contains(r.Text, "from the user's own streams") {
		t.Errorf("custom zones, yet the stored ones shown:\n%s", r.Text)
	}
	if r := callTool(t, h, userB, "activity_analysis", `{"id":4001}`); strings.Contains(r.Text, "the user's own zones") {
		t.Errorf("custom zones, yet the stored ones used:\n%s", r.Text)
	}
}

func loadZones(t *testing.T, svc *service) map[string]storedHRZones {
	t.Helper()
	m, err := svc.store.loadMetrics(userA)
	if err != nil {
		t.Fatal(err)
	}
	return m.HRZones
}

// The stored zones outlive a restart, and a newly read activity recalculates
// them with the date of that calculation.
func TestHRZones_storedAndRecalculated(t *testing.T) {
	f, svc, dir := hrFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "hr_zones", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	first := loadZones(t, svc)["cycling"]

	// A fresh process over the same /data: the profile shows them without
	// listing a single activity.
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	h2 := newServer(config{token: testToken}, svc2, "test")
	lists := f.count("/athlete/activities")
	if r := callTool(t, h2, userA, "profile", `{}`); !strings.Contains(r.Text, "(cycling, calculated 2026-11-03, max 174 bpm") ||
		f.count("/athlete/activities") != lists {
		t.Errorf("profile after a restart:\n%s", r.Text)
	}

	// A day later a harder ride is analysed: 179 bpm confirms the 180 that
	// stood alone, and the zones are recalculated without hr_zones.
	f.advance(24 * time.Hour)
	hard := synthRideWith(1, 1, 9, false)
	f.addActivity(1, 3011, "Hard", "Ride", local(2026, 11, 3, 9), &hard, map[string]any{"max_heartrate": 179.0})
	if r := callTool(t, h2, userA, "activity_analysis", `{"id":3011}`); r.IsError {
		t.Fatal(r.Text)
	}
	now := loadZones(t, svc2)["cycling"]
	if now.MaxHR != 180 || now.MaxAt.ID != 3001 || now.ConfirmAt.ID != 3011 || !now.CalculatedAt.Equal(f.clock()) || !now.CalculatedAt.After(first.CalculatedAt) ||
		now.WithHR != first.WithHR || now.Used != first.Used+1 {
		t.Errorf("after the hard ride: %+v (before %+v)", now, first)
	}
	// A run's analysis leaves the cycling zones and their date alone.
	f.advance(time.Hour)
	if r := callTool(t, h2, userA, "activity_analysis", `{"id":3005}`); r.IsError {
		t.Fatal(r.Text)
	}
	if z := loadZones(t, svc2)["cycling"]; !z.CalculatedAt.Equal(now.CalculatedAt) {
		t.Errorf("a run recalculated the cycling zones: %v", z.CalculatedAt)
	}
}

// Before hr_zones ran, an analysis makes no zones: one easy ride is no
// estimate, and Strava's stay, labelled.
func TestHRZones_noneBeforeAsked(t *testing.T) {
	_, svc, _ := hrFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":3003}`)
	if r.IsError || !strings.Contains(r.Text, "Strava's default zones, usually age-based; hr_zones estimates the user's own") {
		t.Errorf("analysis before hr_zones:\n%s", r.Text)
	}
	if z := loadZones(t, svc); len(z) != 0 {
		t.Errorf("zones made without hr_zones: %+v", z)
	}
}

// Deleted on Strava, or its sport corrected: the activity stops counting.
func TestHRZones_deletedAndCorrected(t *testing.T) {
	f, svc, _ := hrFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "hr_zones", `{"sport":"cycling"}`); r.IsError {
		t.Fatal(r.Text)
	}
	f.mu.Lock()
	var kept []map[string]any
	for _, a := range f.activities[1] {
		switch a["id"] {
		case int64(3002): // the max: deleted
		case int64(3001): // the best 20 min: an e-bike ride after all
			a["sport_type"], a["type"] = "EBikeRide", "EBikeRide"
			kept = append(kept, a)
		default:
			kept = append(kept, a)
		}
	}
	f.activities[1] = kept
	f.mu.Unlock()
	f.advance(10 * time.Minute) // past the cached list
	r := callTool(t, h, userA, "hr_zones", `{"sport":"cycling"}`)
	if r.IsError || !strings.Contains(r.Text, "2 with heart rate, 2 read") || strings.Contains(r.Text, "· Gravel") || strings.Contains(r.Text, "Spike ride") {
		t.Errorf("after delete and correction:\n%s", r.Text)
	}
	m, _ := svc.store.loadMetrics(userA)
	if !m.Activities["3002"].Gone || m.Activities["3001"].Sport != "EBikeRide" {
		t.Errorf("entries: 3002 %+v, 3001 %+v", m.Activities["3002"], m.Activities["3001"])
	}
	if z := m.HRZones["cycling"]; z.Used != 2 || z.MaxHR != 172 || z.Best20At == nil || z.Best20At.ID == 3001 {
		t.Errorf("zones after delete and correction: %+v", z)
	}
}

func TestHRZones_budget(t *testing.T) {
	f, svc, _ := hrFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	f.rateUsed15 = 60 // past the cold budget's share: no stream is read
	r := callTool(t, h, userA, "hr_zones", `{"sport":"cycling"}`)
	if r.IsError || !strings.Contains(r.Text, "4 with heart rate, 0 read; 4 not read yet (the Strava rate budget)") ||
		strings.Contains(r.Text, "max heart rate") {
		t.Errorf("budget:\n%s", r.Text)
	}
	if f.count("/activities/3002/streams") != 0 {
		t.Error("streams read past the budget")
	}
}

// More activities than the list reads: the newest are kept, the oldest of the
// window are said to be left out.
func TestHRZones_longList(t *testing.T) {
	f, svc, _ := hrFixture(t)
	for i := range maxListPages * activitiesPerPage {
		f.addActivity(1, int64(5000+i), "Walk", "Walk", local(2026, 1, 1, 0).Add(time.Duration(i)*time.Hour), nil, nil)
	}
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "hr_zones", `{"sport":"cycling"}`)
	if r.IsError || !strings.Contains(r.Text, "4 with heart rate, 4 read") ||
		!strings.Contains(r.Text, "The list stopped at 600 activities: older ones in the window are left out.") {
		t.Errorf("long list:\n%s", r.Text)
	}
}

// A stream without heart rate is read once; a failed read is retried. The
// running family, fully read, gives no reason of the other's.
func TestHRZones_noHRAndFailures(t *testing.T) {
	f, svc, _ := hrFixture(t)
	noHR := synthRide(false)
	noHR.HR = nil
	f.addActivity(1, 3009, "Strap dead", "Ride", local(2026, 10, 9, 9), &noHR, nil)
	broken := synthRide(false)
	f.addActivity(1, 3010, "Broken", "Ride", local(2026, 10, 10, 9), &broken, nil)
	f.failStreams[3010] = true
	h := newServer(config{token: testToken}, svc, "test")
	for range 2 {
		r := callTool(t, h, userA, "hr_zones", `{}`)
		if r.IsError || !strings.Contains(r.Text, "6 with heart rate, 5 read; 1 failed to read (a later call retries)") ||
			!strings.Contains(r.Text, "Call hr_zones again later") || !strings.Contains(r.Text, "max heart rate: 174 bpm") ||
			!strings.Contains(r.Text, "2 with heart rate, 2 read\n") {
			t.Errorf("failures:\n%s", r.Text)
		}
	}
	if n := f.count("/activities/3009/streams"); n != 1 {
		t.Errorf("a stream without heart rate read %d times", n)
	}
	if n := f.count("/activities/3010/streams"); n < 2 {
		t.Errorf("a failed read not retried: %d", n)
	}
}
