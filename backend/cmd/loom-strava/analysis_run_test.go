package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// On request a run gets the runner's own pace per grade from the window's
// kept streams, no Strava read for it.
func TestAnalysis_gradeProfile(t *testing.T) {
	f, svc, _ := runFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":2001}`); strings.Contains(r.Text, "own grade profile") {
		t.Errorf("a grade profile unasked:\n%s", r.Text)
	}
	before := len(f.seen)
	r := callTool(t, h, userA, "activity_analysis", `{"id":2001,"grade_profile":true}`)
	for _, want := range []string{"own grade profile (median pace per grade, 3 outdoor runs'", "this run adjusted by it",
		`loomcharts.grade_pace("in/strava_2001_grade_pace.csv", "out/grade_pace_2001.png", lang=L)`} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if first, _, _ := strings.Cut(r.file("strava_2001_grade_pace.csv"), "\n"); first != hdrGradePace {
		t.Errorf("grade file header %q", first)
	}
	if n := len(f.seen) - before; n != 0 {
		t.Errorf("the grade profile read Strava %d times: %v", n, f.seen[before:])
	}
}

// One tool call's stream reads share maxColdFetches: a second reader (the
// threshold after the baseline) starts where the first stopped.
func TestReadStreams_perCallLimit(t *testing.T) {
	f, svc, _ := runFixture(t)
	tl := &tools{svc: svc}
	before := f.count("/activities/2002/streams")
	r := tl.readStreams(context.Background(), userA, []summaryActivity{{ID: 2002}}, maxColdFetches)
	if r.stoppedBy != "the per-call limit" || len(r.got) != 0 || f.count("/activities/2002/streams") != before {
		t.Errorf("past the limit: stopped by %q, got %d", r.stoppedBy, len(r.got))
	}
}

// runFixture: Anna (cycling FTP 250) runs 2001 on Sun 2026-11-01 with
// running power; two earlier runs with heart-rate streams (the running
// threshold comes from them) and an earlier ride with power.
func runFixture(t *testing.T) (*fakeStrava, *service, string) {
	t.Helper()
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	f.zones[1].(map[string]any)["heart_rate"].(map[string]any)["custom_zones"] = false
	run := func(id int64, day int, v1, v2 float64) {
		st := synthRun(v1, v2)
		f.addActivity(1, id, "Run", "Run", local(2026, 10, day, 7), &st, map[string]any{"distance": 7500.0, "moving_time": 3000,
			"average_speed": 2.5, "max_speed": 3.04, "average_heartrate": 146.0, "max_heartrate": 160.0, "average_watts": 221.0,
			"weighted_average_watts": 224.0, "device_watts": true, "average_cadence": 81.0,
			"splits_metric": []map[string]any{{"split": 1, "distance": 1000.0, "average_speed": 2.5, "average_grade_adjusted_speed": 2.47}}})
	}
	run(2002, 20, 2.5, 2.5)
	run(2003, 27, 2.45, 2.5)
	ride := synthRide(false)
	f.addActivity(1, 2004, "Ride", "Ride", local(2026, 10, 25, 9), &ride, nil)
	st := synthRun(2.55, 2.5)
	f.addActivity(1, 2001, "Morning run", "Run", local(2026, 11, 1, 7), &st, map[string]any{"distance": 7500.0, "moving_time": 3000,
		"average_speed": 2.51, "max_speed": 3.04, "average_heartrate": 146.0, "max_heartrate": 160.0, "average_watts": 221.0,
		"weighted_average_watts": 224.0, "device_watts": true, "average_cadence": 81.0,
		"splits_metric": []map[string]any{{"split": 1, "distance": 1000.0, "average_speed": 2.5, "average_grade_adjusted_speed": 2.47}}})
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	return f, svc, dir
}

func TestAnalysis_run(t *testing.T) {
	f, svc, dir := runFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":2001}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{
		"summary tables (show them as tables in the answer)", "speed | avg 9.0 km/h", "pace | avg 6:38 /km | grade-adjusted",
		"power | avg 220 W", "W/kg at 74 kg", "running dynamics | cadence avg 162 spm",
		// Strava's FTP is cycling's: no IF, TSS or power zones for a run.
		"(no IF, TSS or power zones for this sport: Strava keeps only a cycling FTP)",
		"cadence avg 162 · max 162 steps/min",
		"threshold heart rate calculated now from the user's streams for running",
		// The zones made in this call bucket this run too, not Strava's defaults.
		"the user's own zones from their streams (running, LTHR",
		"fitness and fatigue on 2026-11-01", "this activity: load ",
		`loomcharts.load_profile("in/strava_2001_baseline.csv", "out/load_profile_6w_2001.png", weeks=6, lang=L)`,
		`loomcharts.fitness_fatigue("in/strava_2001_load.csv", "out/fitness_fatigue_2001.png", lang=L)  (only when asked`,
		`loomcharts.hr_profile("in/strava_2001_streams.csv", "out/hr_profile_2001.png", lang=L)`,
		`loomcharts.split_variation("in/strava_2001_streams.csv", "out/split_variation_2001.png", metric="watts", sport="Run", lang=L)`,
		"In an answer about pain or an injury draw only fitness_fatigue.",
	} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	for _, not := range []string{" · IF ", "time in power zones"} {
		if strings.Contains(r.Text, not) {
			t.Errorf("a run against the cycling FTP: %q\n%s", not, r.Text)
		}
	}
	// The streams file has no power zone for a run; the baseline its load.
	streams := strings.Split(strings.TrimSpace(r.file("strava_2001_streams.csv")), "\n")
	if row := strings.Split(streams[2], ","); row[12] != "" || row[13] == "" {
		t.Errorf("run streams row: power_zone %q hr_zone %q", row[12], row[13])
	}
	base := r.file("strava_2001_baseline.csv")
	if !strings.Contains(base, "2001,2026-11-01,Morning run,Run,1,") || !strings.Contains(base, ",221,\n") {
		t.Errorf("baseline:\n%s", base)
	}
	load := strings.Split(strings.TrimSpace(r.file("strava_2001_load.csv")), "\n")
	if len(load) != loadHistoryDays+1 || load[0] != hdrLoad || !strings.HasPrefix(load[len(load)-1], "2026-11-01,") ||
		strings.HasSuffix(load[len(load)-1], ",,") {
		t.Errorf("load file: %d rows, last %q", len(load), load[len(load)-1])
	}

	// Everything kept: a repeat asks Strava nothing, writes no metric file,
	// and the threshold stays.
	path := filepath.Join(dir, metricsDir, userA+".json")
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.seen)
	r2 := callTool(t, h, userA, "activity_analysis", `{"id":2001}`)
	if r2.IsError || strings.Contains(r2.Text, "calculated now") || !strings.Contains(r2.Text, "this activity: load 92") {
		t.Errorf("repeat:\n%s", r2.Text)
	}
	if again, err := os.Stat(path); err != nil || !os.SameFile(stat, again) {
		t.Error("a repeat analysis rewrote the metric file")
	}
	if n := len(f.seen) - before; n != 0 {
		t.Errorf("a repeat made %d Strava calls: %v", n, f.seen[before:])
	}
	// A new process over the same /data reads no activity again.
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	before = len(f.seen)
	if r3 := callTool(t, newServer(config{token: testToken}, svc2, "test"), userA, "activity_analysis", `{"id":2001}`); r3.IsError {
		t.Fatal(r3.Text)
	}
	for _, call := range f.seen[before:] {
		// Strava's own zones refusal (Summit) is remembered in memory only.
		if strings.HasSuffix(call, "/zones") {
			continue
		}
		if strings.Contains(call, "/activities/") || strings.Contains(call, "/athlete/activities") {
			t.Errorf("after a restart: %s", call)
		}
	}
}

func TestAnalysis_swim(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	d := poolSwim()
	laps := []map[string]any{}
	for _, l := range d.Laps {
		laps = append(laps, map[string]any{"distance": l.Distance, "moving_time": l.MovingTime, "elapsed_time": l.ElapsedTime,
			"average_cadence": l.AverageCadence})
	}
	f.addActivity(1, 3001, "Pool", "Swim", local(2026, 11, 1, 7), nil, map[string]any{"distance": 300.0, "moving_time": 430,
		"elapsed_time": 548, "average_speed": 0.7, "average_cadence": 22.0, "laps": laps, "device_watts": false,
		"weighted_average_watts": 0.0, "average_watts": 0.0, "start_latlng": []float64{}, "end_latlng": []float64{}})
	ow := synthRide(false) // a GPS track
	f.addActivity(1, 3002, "Lake", "Swim", local(2026, 10, 31, 7), &ow, map[string]any{"distance": 1000.0, "average_cadence": 26.0,
		"laps": []map[string]any{{"distance": 500.0, "moving_time": 600}, {"distance": 500.0, "moving_time": 620}}})
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	r := callTool(t, h, userA, "activity_analysis", `{"id":3001}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{"swim summary tables", "12 lengths of 25 m", "SWOLF ≈ ", "intervals (swim |", "this activity: no load (no load for this sport)"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if strings.Contains(r.Text, "laps (") {
		t.Errorf("the swim's laps twice:\n%s", r.Text)
	}
	// Open water keeps the device's laps: the swim summary lists none there.
	if r := callTool(t, h, userA, "activity_analysis", `{"id":3002}`); r.IsError || !strings.Contains(r.Text, "open water (a GPS track)") ||
		!strings.Contains(r.Text, "laps (2):") {
		t.Errorf("open water:\n%s", r.Text)
	}
	if !strings.Contains(r.Text, "critical swim speed: needs a 400 and a 200 lap (m or yd)") {
		t.Errorf("CSS without a 400:\n%s", r.Text)
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":3001,"pool_m":33}`); !r.IsError {
		t.Error("pool_m 33 accepted")
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":3001,"pool_m":50}`); !strings.Contains(r.Text, "7 lengths of 50 m") ||
		!strings.Contains(r.Text, "pool 50 m (as the user said)") {
		t.Errorf("pool_m 50:\n%s", r.Text)
	}
}

// list_activities gives the fitness and fatigue at its window's end: a pain
// question reads it first.
func TestListActivities_load(t *testing.T) {
	f, svc, _ := runFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	// The user's clock is ahead of the server's (now 2026-11-03 10:02 UTC):
	// their run on their 4 November still counts.
	f.addActivity(1, 2010, "Early run", "Run", local(2026, 11, 4, 6), nil, nil)
	if r := callTool(t, h, userA, "list_activities", `{}`); !strings.Contains(r.Text, "fitness and fatigue on 2026-11-04") {
		t.Errorf("window end:\n%s", r.Text)
	}
	r := callTool(t, h, userA, "list_activities", `{"after":"2026-10-04","before":"2026-11-01"}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{"fitness and fatigue on 2026-10-31", "CTL ", "ACWR", "load from 1 of 3 activities",
		"Run 2 (no running threshold heart rate yet)", "strava_load.csv"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if first, _, _ := strings.Cut(r.file("strava_load.csv"), "\n"); first != hdrLoad {
		t.Errorf("load file header %q", first)
	}
}
