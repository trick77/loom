package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEfficiencyOf(t *testing.T) {
	run := summaryActivity{SportType: "Run", Distance: 10000, TotalElevationGain: 100, MovingTime: 3000, HasHeartrate: true, AverageHeartrate: 150}
	// (10,000 + 10 × 100) m in 50 min, per bpm.
	if v := efficiencyOf(run); !within(v, 11000.0/50/150, 1e-9) {
		t.Errorf("run EF %v", v)
	}
	ride := summaryActivity{SportType: "Ride", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: 140, DeviceWatts: true, WeightedAverageWatts: 210}
	if v := efficiencyOf(ride); !within(v, 1.5, 1e-9) {
		t.Errorf("ride EF from weighted power %v", v)
	}
	ride.DeviceWatts = false
	if v := efficiencyOf(ride); !math.IsNaN(v) {
		t.Errorf("a ride without power has no EF: %v", v)
	}
	run.HasHeartrate = false
	if v := efficiencyOf(run); !math.IsNaN(v) {
		t.Errorf("no heart rate, no EF: %v", v)
	}
}

// effFixture: Anna's easy runs over a year get faster at the same heart
// rate; a few hard ones stay out of the trend.
func effFixture(t *testing.T) (*fakeStrava, *service) {
	t.Helper()
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250) // Strava's custom zones: Z2 123–153
	for i := range 12 {
		day := local(2026, 10, 28, 7).AddDate(0, -i, 0)
		f.addActivity(1, int64(6000+i), fmt.Sprintf("Easy %d", i), "Run", day, nil, map[string]any{"distance": 10000.0,
			"moving_time": 3300 + i*30, "total_elevation_gain": 50.0, "average_heartrate": 140.0})
		f.addActivity(1, int64(6100+i), fmt.Sprintf("Tempo %d", i), "Run", day.AddDate(0, 0, 2), nil, map[string]any{"distance": 10000.0,
			"moving_time": 2700, "total_elevation_gain": 50.0, "average_heartrate": 168.0})
	}
	f.addActivity(1, 6200, "Treadmill", "VirtualRun", local(2026, 10, 20, 7), nil, map[string]any{"average_heartrate": 140.0})
	svc, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	return f, svc
}

func TestEfficiencyTrend(t *testing.T) {
	f, svc := effFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "efficiency_trend", `{"sport":"running"}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{
		"aerobic efficiency of the user's outdoor runs", "24 with heart rate, 12 easy (average heart rate below 153 bpm, where Z3 starts",
		"their custom Strava zones", "monthly median, easy only:", "1 indoor in the window left out",
		`loomcharts.efficiency_trend("in/strava_efficiency.csv", "out/efficiency_trend_running_12m.png", sport="running", lang=L)`,
	} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	// The easy runs got faster at the same heart rate: a rise, n 3 and 3.
	if !regexp.MustCompile(`last 3 months: [0-9.]+ against [0-9.]+ in the 3 months before: \+[0-9.]+ % \(n 3 and 3\)`).MatchString(r.Text) {
		t.Errorf("trend:\n%s", r.Text)
	}
	// A short window still compares its full two periods.
	if r := callTool(t, h, userA, "efficiency_trend", `{"sport":"running","months":1}`); !strings.Contains(r.Text, "(n 3 and 3)") {
		t.Errorf("months 1:\n%s", r.Text)
	}
	csv := r.file("strava_efficiency.csv")
	if first, _, _ := strings.Cut(csv, "\n"); first != hdrEfficiency || strings.Count(csv, "\n") != 25 {
		t.Errorf("efficiency file:\n%s", csv)
	}
	// Summaries only: not one stream or detail read.
	for _, c := range f.seen {
		if strings.Contains(c, "/activities/") {
			t.Errorf("read %s", c)
		}
	}
	before := len(f.seen)
	callTool(t, h, userA, "efficiency_trend", `{"sport":"running"}`)
	if len(f.seen) != before {
		t.Errorf("a repeat made %d calls", len(f.seen)-before)
	}
	for _, bad := range []string{`{}`, `{"sport":"swim"}`, `{"sport":"running","months":30}`} {
		if r := callTool(t, h, userA, "efficiency_trend", bad); !r.IsError {
			t.Errorf("%s accepted", bad)
		}
	}
	f.advance(time.Hour)
	if r := callTool(t, h, userA, "efficiency_trend", `{"sport":"cycling"}`); r.IsError || !strings.Contains(r.Text, "no outdoor cycling activities with heart rate and a power meter") {
		t.Errorf("no rides:\n%s", r.Text)
	}

	// Strava's zones are defaults: the user's own replace them (as in
	// activity_analysis); custom ones above won over own ones.
	f.mu.Lock()
	f.zones[1].(map[string]any)["heart_rate"].(map[string]any)["custom_zones"] = false
	f.mu.Unlock()
	own := storedHRZones{CalculatedAt: f.clock(), LTHR: 160, Zones: []zoneRange{{0, 135}, {136, 144}, {145, 151}, {152, 159}, {160, -1}}}
	if err := svc.store.saveMetrics(userA, metricFile{Athlete: 1, Activities: map[string]storedMetrics{}, HRZones: map[string]storedHRZones{"running": own}}); err != nil {
		t.Fatal(err)
	}
	f.advance(2 * time.Hour) // Strava's zones read again
	if r := callTool(t, h, userA, "efficiency_trend", `{"sport":"running"}`); !strings.Contains(r.Text, "below 145 bpm, where Z3 starts in their own zones") {
		t.Errorf("own zones over Strava's defaults:\n%s", r.Text)
	}
}
