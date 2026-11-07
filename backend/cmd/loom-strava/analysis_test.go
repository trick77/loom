package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func local(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// analysisFixture: Anna (FTP 250) rides 1001 on Sun 2026-11-01. In the six
// weeks before: two rides on the same loop (1002, 1008), one elsewhere
// (1003), a Zwift ride (1004), a run (1005); outside the window 1006 (too
// old) and 1007 (after).
func analysisFixture(t *testing.T) (*fakeStrava, *service, string) {
	t.Helper()
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	this, same1, other, virt, same2 := synthRideWith(1.05, 1.03, 0, false), synthRideWith(1, 0.97, 0, false),
		synthRideWith(0.9, 1, 0, false), synthRideWith(1.1, 1, 0, false), synthRideWith(0.95, 0.95, 0, false)
	f.addActivity(1, 1001, "Albis loop", "Ride", local(2026, 11, 1, 9), &this, map[string]any{"average_speed": 7.0, "total_elevation_gain": 130.0})
	f.addActivity(1, 1002, "Albis loop", "Ride", local(2026, 10, 28, 9), &same1, map[string]any{"average_speed": 6.5})
	f.addActivity(1, 1003, "Greifensee", "Ride", local(2026, 10, 20, 9), &other, map[string]any{"average_speed": 6.0, "end_latlng": []float64{47.5, 8.5}})
	f.addActivity(1, 1004, "Zwift Watopia", "VirtualRide", local(2026, 10, 22, 18), &virt, map[string]any{"average_speed": 9.0, "trainer": true})
	f.addActivity(1, 1005, "Lunch run", "Run", local(2026, 10, 23, 12), nil, map[string]any{"average_speed": 3.2, "device_watts": false, "weighted_average_watts": 0.0})
	f.addActivity(1, 1006, "Old ride", "Ride", local(2026, 9, 1, 9), &other, nil)
	f.addActivity(1, 1007, "Later ride", "Ride", local(2026, 11, 2, 9), &other, nil)
	f.addActivity(1, 1008, "Albis loop", "Ride", local(2026, 10, 10, 9), &same2, map[string]any{"average_speed": 6.8})
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	return f, svc, dir
}

func TestAnalysis_comparison(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`)
	if r.IsError {
		t.Fatalf("analysis: %s", r.Text)
	}
	text := r.Text
	for _, want := range []string{
		"[1] Activity 1001 · Albis loop · Ride · Sun 2026-11-01 09:00",
		"power: avg", "NP", "IF", "TSS", "(FTP 250 W, 74 kg)",
		"heart rate: avg", "decoupling",
		"time in power zones Z1..Z5+", "time in HR zones Z1..Z5",
		"(computed from the streams: Strava's own activity zones needs a Strava subscription)", // 402
		"mean-max power: 5 s",
		"climbs (≥ 500 m at ≥ 3 %):", "VAM",
		"hard efforts (≥ 30 s above 105 % FTP",
		// Baseline: the three outdoor rides of the window; Zwift, the run,
		// the old, the later ride and the ride itself are out.
		"compared with 3 rides (Ride), 2026-09-14 – 2026-10-31",
		"indoor and outdoor kept apart",
		"Avg speed | 25.2 km/h | 23.4 km/h | +8 % | 1st best of 4 · best in window",
		// Every ride is 10 km: a shared first place gets no flag.
		"Distance | 10.0 km | 10.0 km | +0 % | 1st highest of 4\n",
		"HR drift (decoupling) |",
		"Normalized power |",
		"same route (start and end within 200 m",
		"[2] 2026-10-28 · Albis loop", "[3] 2026-10-10 · Albis loop",
		"ahead at the finish",
		"load: this week (Mon–Sun, up to this activity) TSS",
		"weeks: W38",
		"per km (km | time | speed | W | bpm | rpm | alt | grade):",
		"attached for run_python: strava_1001_streams.csv",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("analysis lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"Zwift", "Lunch run", "Old ride", "Later ride", "Greifensee"} {
		if strings.Contains(text, not) {
			t.Errorf("analysis mentions %q", not)
		}
	}
	if len(r.Sources) != 3 || r.Sources[0]["url"] != "https://www.strava.com/activities/1001" || r.Sources[1]["url"] != "https://www.strava.com/activities/1002" {
		t.Errorf("sources = %v", r.Sources)
	}

	// The file contract, header by header.
	heads := map[string]string{
		"strava_1001_streams.csv":     hdrStreams,
		"strava_1001_climbs.csv":      hdrClimbs,
		"strava_1001_power_curve.csv": hdrPowerCurve,
		"strava_1001_baseline.csv":    hdrBaseline,
		"strava_1001_same_route.csv":  "distance_km,gap_s_2026-10-28,gap_s_2026-10-10",
		"strava_weeks.csv":            hdrWeeks,
	}
	if len(r.Files) != len(heads) || r.Files[0].URI != "file:///strava_1001_streams.csv" {
		t.Errorf("files = %d, the streams first: %v", len(r.Files), r.Files[0].URI)
	}
	for name, head := range heads {
		body := r.file(name)
		if first, _, _ := strings.Cut(body, "\n"); first != head {
			t.Errorf("%s header = %q, want %q", name, first, head)
		}
	}
	if hdrStreams != "elapsed_s,distance_km,lat,lng,altitude_m,speed_kmh,hr,cadence,watts,grade_pct,temp_c,moving,power_zone,hr_zone" ||
		hdrClimbs != "n,from_km,to_km,len_km,gain_m,grade_pct,secs,vam,avg_w,wkg,avg_hr" ||
		hdrPowerCurve != "duration_s,this_w,window_best_w,window_median_w" ||
		hdrBaseline != "activity_id,date,name,sport_type,is_this,same_route,distance_km,moving_s,avg_kmh,gain_m,np_w,tss,ef,decoupling_pct,best_20m_w,avg_hr" ||
		hdrWeeks != "week_start,iso_week,sport_type,count,distance_km,moving_s,gain_m,tss" ||
		hdrActivities != "activity_id,date,name,sport_type,trainer,distance_km,moving_s,gain_m,avg_kmh,avg_hr,avg_w,weighted_w,kj,suffer_score" {
		t.Error("a CSV header drifted from the chart module's contract")
	}
	base := strings.Split(strings.TrimSpace(r.file("strava_1001_baseline.csv")), "\n")
	if len(base) != 5 || !strings.HasPrefix(base[1], "1001,2026-11-01,Albis loop,Ride,1,0,") || !strings.HasPrefix(base[2], "1002,2026-10-28,Albis loop,Ride,0,1,") {
		t.Errorf("baseline rows:\n%s", strings.Join(base, "\n"))
	}
	streams := strings.Split(strings.TrimSpace(r.file("strava_1001_streams.csv")), "\n")
	if row := strings.Split(streams[2], ","); len(row) != 14 || row[0] != "5" || row[11] != "1" || row[12] == "" || row[13] == "" {
		t.Errorf("streams row: %v", row)
	}
	curve := strings.Split(strings.TrimSpace(r.file("strava_1001_power_curve.csv")), "\n")
	if len(curve) != 13 || !strings.HasPrefix(curve[len(curve)-1], "5400,,") {
		t.Errorf("power curve: %v", curve)
	}
	gaps := strings.Split(strings.TrimSpace(r.file("strava_1001_same_route.csv")), "\n")
	if row := strings.Split(gaps[len(gaps)/2], ","); len(row) != 3 || !strings.HasPrefix(row[1], "-") {
		t.Errorf("same-route row (this ride is faster: negative gap): %v", row)
	}
	weeks := strings.Split(strings.TrimSpace(r.file("strava_weeks.csv")), "\n")
	if !strings.Contains(r.file("strava_weeks.csv"), "2026-10-26,2026-W44,Ride,2,") || len(weeks) < 8 {
		t.Errorf("weeks:\n%s", r.file("strava_weeks.csv"))
	}

	// The metric store now holds the ride and its baseline, never streams.
	b, err := os.ReadFile(filepath.Join(dir, metricsDir, userA+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{`"1001"`, `"1002"`, `"1003"`, `"1008"`} {
		if !strings.Contains(string(b), id) {
			t.Errorf("metric store lacks %s", id)
		}
	}
	if strings.Contains(string(b), `"1004"`) || len(b) > 4000 {
		t.Errorf("metric store holds the wrong things (%d bytes): %s", len(b), b)
	}

	// A fresh process over the same /data: no baseline streams again.
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	h2 := newServer(config{token: testToken}, svc2, "test")
	before := f.count("/activities/1003/streams")
	if r := callTool(t, h2, userA, "activity_analysis", `{"id":1001}`); r.IsError || strings.Contains(r.Text, "partial") {
		t.Fatalf("second analysis: %s", r.Text)
	}
	if f.count("/activities/1003/streams") != before {
		t.Error("the metric store was not reused: baseline streams fetched again")
	}
}

// A real-sized ride (the prototype's 74 km, 2.5 h at 1 Hz) stays well
// inside mcpserver's caps: 24 KB of text, 3 MiB of files.
func TestAnalysis_realSizedRide(t *testing.T) {
	fh, err := os.Open("testdata/albis_streams.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatal(err)
	}
	var set streamSet
	if err := json.NewDecoder(zr).Decode(&set); err != nil {
		t.Fatal(err)
	}
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 265)
	f.addActivity(1, 1234567890, "Albis loop", "Ride", local(2026, 11, 1, 9), &set, map[string]any{"distance": 74300.0, "moving_time": 8955})
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1234567890}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	size := 0
	for _, x := range r.Files {
		size += len(x.Text)
	}
	if len(r.Text) > 20<<10 || size > maxFilesBytes || strings.Contains(r.Text, "[output truncated]") {
		t.Errorf("text %d bytes, files %d bytes", len(r.Text), size)
	}
	for _, want := range []string{"NP 208 W", "TSS 153", "climbs (≥ 500 m at ≥ 3 %):\n  1. km 18.0–24.3 · 6.3 km · 383 m · 6.1 %", "2. km 47.0–51.9", "per km (km |", "74.0 | "} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	rows := strings.Count(r.file("strava_1234567890_streams.csv"), "\n") - 1
	if rows < 1700 || rows > 1900 {
		t.Errorf("streams rows = %d, want a 5 s resample of ~9,100 s", rows)
	}
}

// A repeat analysis writes no metric file (nothing changed), and a refused
// Summit zones read is not asked again, for any activity.
func TestAnalysis_repeatCostsNothing(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	path := filepath.Join(dir, metricsDir, userA+".json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError || !strings.Contains(r.Text, msgSubscription) {
		t.Fatalf("repeat: %s", r.Text)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Error("a repeat analysis rewrote the metric file") // fsx replaces it by rename
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1002}`); r.IsError {
		t.Fatal(r.Text)
	}
	if n := f.count("/activities/1001/zones") + f.count("/activities/1002/zones"); n != 1 {
		t.Errorf("zones calls = %d, want the one refusal", n)
	}
}

// A token answer without the athlete keeps the one known, and the metrics
// with it; a connection that never learnt its athlete takes it from
// /athlete, and another athlete's metric file goes.
func TestMetrics_athleteGuard(t *testing.T) {
	f, svc, dir := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	path := filepath.Join(dir, metricsDir, userA+".json")
	f.noAthlete = true
	f.addAthlete(1, "Anna", "code-a2", 250)
	connect(t, h, userA, "code-a2")
	if c, _ := svc.conn(userA); c.AthleteID != 1 || c.AthleteName != "Anna Test" {
		t.Errorf("athlete lost on reconnect: %+v", c)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the same athlete's metrics were wiped")
	}

	// A connection without an athlete, and a metric file of athlete 2.
	svc.mu.Lock()
	c := svc.st.Users[userA]
	c.AthleteID = 0
	svc.st.Users[userA] = c
	svc.mu.Unlock()
	if err := svc.store.saveMetrics(userA, metricFile{Athlete: 2, Activities: map[string]storedMetrics{"9999": {V: metricsVersion}}}); err != nil {
		t.Fatal(err)
	}
	tl := &tools{svc: svc}
	tl.storeMetrics(userA, 1, map[string]storedMetrics{"1": {V: metricsVersion}}, nil, nil)
	if m, _ := svc.store.loadMetrics(userA); m.Athlete != 2 || len(m.Activities) != 1 {
		t.Errorf("athlete 1's metrics went into athlete 2's file: %+v", m)
	}
	if got := tl.loadMetrics(userA, 1); len(got) != 0 {
		t.Errorf("athlete 1 read athlete 2's metrics: %v", got)
	}
	f.advance(2 * time.Hour) // the cached /athlete expires
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); r.IsError {
		t.Fatal(r.Text)
	}
	if c, _ := svc.conn(userA); c.AthleteID != 1 {
		t.Errorf("athlete not learnt: %+v", c)
	}
	m, _ := svc.store.loadMetrics(userA)
	if _, foreign := m.Activities["9999"]; foreign || m.Athlete != 1 || len(m.Activities) == 0 {
		t.Errorf("metric file after learning: athlete %d, %d entries, foreign %v", m.Athlete, len(m.Activities), foreign)
	}
}

func TestAnalysis_mixIndoorAndWeeks(t *testing.T) {
	_, svc, _ := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"mix_indoor":true,"weeks":2}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	// Two weeks before W44: 1002 (W44), 1003, 1004 (W43).
	if !strings.Contains(r.Text, "compared with 3 rides (Ride + VirtualRide), 2026-10-12") || strings.Contains(r.Text, "kept apart") {
		t.Errorf("mix_indoor: %s", r.Text)
	}
	if !strings.Contains(r.Text, "Avg speed | 25.2 km/h | 23.4 km/h | +8 % | 2nd best of 4") {
		t.Errorf("rank with Zwift's 32 km/h ahead:\n%s", r.Text)
	}
	for _, bad := range []string{`{"id":1001,"weeks":27}`, `{"id":1001,"weeks":-1}`, `{"id":"x"}`, `{"id":1001,"from_km":5,"to_km":2}`, `{"id":1001,"bogus":1}`} {
		if r := callTool(t, h, userA, "activity_analysis", bad); !r.IsError {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestAnalysis_section(t *testing.T) {
	_, svc, _ := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"from_km":5,"to_km":7}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	// Rows and climbs give the ride's own km, not the section's.
	if !strings.Contains(r.Text, "section km 5.0–7.0") || !strings.Contains(r.Text, "climbs (≥ 500 m") || !strings.Contains(r.Text, "\n6.0 | ") {
		t.Errorf("section:\n%s", r.Text)
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1001,"from_km":50,"to_km":60}`); !r.IsError || !strings.Contains(r.Text, "no data between") {
		t.Errorf("section past the end: %+v", r)
	}
}

// Under a tight 15-minute budget the baseline stops at half of it and says
// what it lacks; a later call fills the rest.
func TestAnalysis_partialBaseline(t *testing.T) {
	f, svc, _ := analysisFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	f.mu.Lock()
	f.rateLimit15 = 14 // the six calls before the baseline spend 6 of 14
	f.mu.Unlock()
	r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	if !strings.Contains(r.Text, "partial: stream metrics (NP, IF, TSS, EF, decoupling, power bests) from 1 of 3 rides, stopped by the Strava rate budget") {
		t.Errorf("partial:\n%s", r.Text)
	}
	if base := r.file("strava_1001_baseline.csv"); !strings.Contains(base, "1003,2026-10-20,Greifensee,Ride,0,0,10.00,1500,21.6,120,,,,,,140") {
		t.Errorf("unknown stream metrics must be empty cells:\n%s", base)
	}
	f.advance(16 * time.Minute)
	f.mu.Lock()
	f.rateUsed15 = 0
	f.mu.Unlock()
	r = callTool(t, h, userA, "activity_analysis", `{"id":1001}`)
	if strings.Contains(r.Text, "partial") {
		t.Errorf("second call still partial:\n%s", r.Text)
	}
}

func TestAnalysis_summitZonesAndFTPChange(t *testing.T) {
	f, svc, _ := analysisFixture(t)
	f.summit[1] = true
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1001}`)
	if !strings.Contains(r.Text, "(HR zone times from Strava)") || !strings.Contains(r.Text, "15 % (1:40)") {
		t.Errorf("summit zones:\n%s", r.Text)
	}
	tssAt := func(text, id string) string {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, id+",") {
				return strings.Split(line, ",")[11]
			}
		}
		return ""
	}
	before := tssAt(r.file("strava_1001_baseline.csv"), "1003")
	// FTP goes up: TSS is recomputed from the stored NP, no stream refetched.
	f.mu.Lock()
	f.athletes[1]["ftp"] = 300.0
	f.mu.Unlock()
	f.advance(61 * time.Minute)
	streams := f.count("/activities/1003/streams")
	r = callTool(t, h, userA, "activity_analysis", `{"id":1001}`)
	after := tssAt(r.file("strava_1001_baseline.csv"), "1003")
	if before == "" || after == "" || before == after {
		t.Errorf("TSS before %q after %q", before, after)
	}
	if f.count("/activities/1003/streams") != streams {
		t.Error("an FTP change refetched streams")
	}
}

func TestAnalysis_noStreamsAndFirstActivity(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 0)
	f.addActivity(1, 1, "Pool", "Swim", local(2026, 11, 1, 7), nil, map[string]any{"average_speed": 0.9, "has_heartrate": false,
		"average_watts": 0.0, "device_watts": false, "weighted_average_watts": 0.0, "start_latlng": []float64{}, "end_latlng": []float64{}})
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	r := callTool(t, h, userA, "activity_analysis", `{"id":1}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{"no streams recorded", "no earlier swims in the window", "1:51 /100m"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if r.file("strava_1_streams.csv") != "" {
		t.Error("a streams file without streams")
	}
	if r := callTool(t, h, userA, "activity_analysis", `{"id":1,"from_km":0,"to_km":1}`); !r.IsError {
		t.Error("a section without a distance stream")
	}
}

func TestListActivities(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	start := local(2026, 1, 1, 8)
	for i := range 250 {
		sport := "Ride"
		if i%5 == 0 {
			sport = "VirtualRide"
		}
		f.addActivity(1, int64(10000+i), fmt.Sprintf("Ride %d", i), sport, start.Add(time.Duration(i)*24*time.Hour), nil, map[string]any{"trainer": sport == "VirtualRide"})
	}
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")

	// The newest 30 need one page: the history is not read for them.
	r := callTool(t, h, userA, "list_activities", `{}`)
	if r.IsError || !strings.Contains(r.Text, "newest first: 30\n") || !strings.Contains(r.Text, "[1] Mon 2026-09-07 08:00 · Ride 249") ||
		f.count("/athlete/activities") != 1 {
		t.Fatalf("default list (%d list calls):\n%s", f.count("/athlete/activities"), r.Text)
	}
	if callTool(t, h, userA, "list_activities", `{}`); f.count("/athlete/activities") != 1 {
		t.Error("a repeat list read Strava again")
	}
	if len(r.Sources) != maxSources || !strings.Contains(r.Text, "\n- ") {
		t.Errorf("sources = %d; items past 20 get no marker", len(r.Sources))
	}
	if !strings.Contains(r.Text, "attached for run_python: strava_activities.csv (30 rows), strava_weeks.csv") {
		t.Errorf("files line:\n%s", r.Text)
	}
	if first, _, _ := strings.Cut(r.file("strava_activities.csv"), "\n"); first != hdrActivities {
		t.Errorf("activities header %q", first)
	}

	// A filter reads on to the end (the second page): two calls in all.
	r = callTool(t, h, userA, "list_activities", `{"limit":500,"sport_type":"virtualride"}`)
	if !strings.Contains(r.Text, "newest first: 50\n") || f.count("/athlete/activities") != 2 {
		t.Errorf("paged filter (%d list calls):\n%.300s", f.count("/athlete/activities"), r.Text)
	}
	r = callTool(t, h, userA, "list_activities", `{"after":"2026-03-01","before":"2026-03-08","trainer_only":true}`)
	if !strings.Contains(r.Text, "newest first: 2\n") || !strings.Contains(r.Text, "[1] Sat 2026-03-07") || strings.Contains(r.Text, "Sun 2026-03-08") {
		t.Errorf("window:\n%s", r.Text)
	}
	r = callTool(t, h, userA, "list_activities", `{"after":"2030-01-01"}`)
	if !strings.HasPrefix(r.Text, "No Strava activities match") {
		t.Errorf("empty: %s", r.Text)
	}
	for _, bad := range []string{`{"after":"March"}`, `{"before":"x"}`, `{"after":"2026-03-08","before":"2026-03-01"}`, `{"limit":"x"}`} {
		if r := callTool(t, h, userA, "list_activities", bad); !r.IsError {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestListActivities_imperialAndTotals(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	f.athletes[1]["measurement_preference"] = "feet"
	f.addActivity(1, 1, "Ride", "Ride", local(2026, 10, 1, 8), nil, nil)
	f.addActivity(1, 2, "Run", "Run", local(2026, 10, 9, 8), nil, map[string]any{"distance": 5000.0, "average_speed": 3.0})
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")
	r := callTool(t, h, userA, "list_activities", `{}`)
	for _, want := range []string{"6.2 mi", "394 ft", "8:56 /mi", "totals: Run 1", "weeks: 2026-W40"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
}

func TestSimpleTools(t *testing.T) {
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	ride := synthRide(false)
	f.addActivity(1, 1001, "Albis loop", "Ride", local(2026, 11, 1, 9), &ride, map[string]any{
		"description": "Windy.", "private_note": "Legs heavy", "calories": 800.0, "perceived_exertion": 6.0, "photos": map[string]any{"count": 2},
		"laps":            []map[string]any{{"distance": 5000.0, "moving_time": 600, "average_speed": 8.3, "average_watts": 200.0}, {"distance": 5000.0, "moving_time": 900, "average_speed": 5.5, "average_watts": 260.0}},
		"splits_metric":   []map[string]any{{"split": 1, "distance": 1000.0, "average_speed": 8.3, "average_heartrate": 130.0, "elevation_difference": 2.0}},
		"best_efforts":    []map[string]any{{"name": "1k", "elapsed_time": 200, "pr_rank": 1}},
		"segment_efforts": []map[string]any{{"name": "Albis climb", "distance": 2000.0, "elapsed_time": 480, "average_watts": 300.0, "pr_rank": 1, "kom_rank": 7, "segment": map[string]any{"id": 77, "average_grade": 6.0}}},
	})
	ride2 := synthRideWith(0.9, 0.9, 5, false)
	f.addActivity(1, 1002, "Albis loop", "Ride", local(2026, 10, 1, 9), &ride2, nil)
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")

	cases := []struct {
		tool, args string
		want       []string
		sources    int
	}{
		{"profile", `{}`, []string{"[1] Strava profile: Anna Test · Zurich, Switzerland · units metric", "FTP 250 W · weight 74.0 kg · 3.38 W/kg",
			"bikes: Anna's bike (id b1) 12,345 km primary", "heart-rate zones (custom): Z1 0–123 bpm", "Z5 184+ bpm",
			"power zones: none from Strava; analyses use Coggan's from FTP"}, 1},
		{"totals", `{}`, []string{"visible to Everyone", "last 4 weeks:", "ride: 8 · 412.0 km", "run: none", "longest ride: 180.0 km", "biggest climb: 1,200 m"}, 0},
		{"get_activity", `{"id":"1001"}`, []string{"[1] Activity 1001 · Albis loop · Ride", "description: Windy.", "private note: Legs heavy", "800 kcal",
			"perceived exertion 6/10", "2 photos", "3 kudos · 1 comment", "laps (2):", "splits per km (1): 1 29.9 km/h HR 130 +2 m",
			"best efforts: 1k 3:20 (PR 1st)", "Albis climb · 2.0 km · 8:00 · 6.0 % · 300 W · PR 1st · KOM rank 7 · segment 77"}, 1},
		{"compare_activities", `{"ids":[1001,1002]}`, []string{"[1] Sun 2026-11-01 09:00 · Albis loop", "[2] Thu 2026-10-01 09:00", "metric | [1] | [2]",
			"Normalized power |", "all on the same route"}, 2},
		// The device's laps, with power as a share of FTP (250 W).
		{"activity_analysis", `{"id":1001}`, []string{"laps (2):\n  1. 5.0 km · 10:00 · 29.9 km/h · 200 W (80 % FTP)\n", "2. 5.0 km · 15:00 · 19.8 km/h · 260 W (104 % FTP)\n"}, 2},
		{"gear", `{}`, []string{"Anna's bike (id b1) · 12,345 km · Canyon Ultimate CF SLX · road · primary", "Anna's shoes"}, 0},
		{"gear", `{"gear_id":"b1"}`, []string{"Anna's bike"}, 0},
		{"routes", `{}`, []string{"[1] Albis loop · ride · 74.3 km · 880 m · est. 2:30:00 · starred · id 3141592653589793238"}, 1},
		{"routes", `{"route_id":"3141592653589793238"}`, []string{"[1] Route 3141592653589793238 · Albis loop", "Albis climb · 6.4 km · 6.1 % · segment 77"}, 1},
		{"segments", `{}`, []string{"[1] Albis climb · Ride · 6.4 km · 6.1 % · PR 22:35 on 2026-11-01 · 12 efforts · id 77"}, 1},
		{"segments", `{"segment_id":77}`, []string{"[1] Segment 77 · Albis climb", "the user's PR 22:35 on 2026-11-01 · 12 efforts",
			"effort history on a segment needs a Strava subscription (Summit); only the PR is shown"}, 1},
	}
	for _, c := range cases {
		r := callTool(t, h, userA, c.tool, c.args)
		if r.IsError {
			t.Errorf("%s %s: %s", c.tool, c.args, r.Text)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(r.Text, w) {
				t.Errorf("%s %s lacks %q:\n%s", c.tool, c.args, w, r.Text)
			}
		}
		if len(r.Sources) != c.sources {
			t.Errorf("%s %s: %d sources, want %d", c.tool, c.args, len(r.Sources), c.sources)
		}
	}
	// Summit: the effort history, each effort citing its activity. The
	// refusal above is remembered for a day.
	f.summit[1] = true
	f.advance(2 * time.Hour)
	if r := callTool(t, h, userA, "segments", `{"segment_id":77}`); !strings.Contains(r.Text, msgSubscription) || f.count("/segment_efforts") != 1 {
		t.Errorf("a remembered refusal (%d calls): %s", f.count("/segment_efforts"), r.Text)
	}
	f.advance(23 * time.Hour)
	r := callTool(t, h, userA, "segments", `{"segment_id":77}`)
	if !strings.Contains(r.Text, "[2] 2026-11-01 · 22:35 · 259 W · PR 1st · activity 2001") || len(r.Sources) != 3 ||
		r.Sources[1]["url"] != "https://www.strava.com/activities/2001" {
		t.Errorf("summit efforts (%d sources):\n%s", len(r.Sources), r.Text)
	}
	errs := []struct{ tool, args, want string }{
		{"get_activity", `{}`, "needs an activity id"},
		{"get_activity", `{"id":424242}`, "not one of the user's activities"},
		{"compare_activities", `{"ids":[1001]}`, "2 to 5"},
		{"compare_activities", `{"ids":[1001,"x"]}`, "every id"},
		{"compare_activities", `{"ids":[1001,424242]}`, "not one of the user's activities"},
		{"gear", `{"gear_id":"b999"}`, "not one of the user's bikes"},
		{"routes", `{"route_id":"999"}`, "not one of the user's own routes"},
		{"routes", `{"route_id":"x"}`, "route_id must be"},
		{"segments", `{"segment_id":"x"}`, "segment_id must be"},
		{"segments", `{"nope":1}`, "segments:"},
	}
	for _, e := range errs {
		if r := callTool(t, h, userA, e.tool, e.args); !r.IsError || !strings.Contains(r.Text, e.want) {
			t.Errorf("%s %s: %+v, want error %q", e.tool, e.args, r, e.want)
		}
	}
	if r := callTool(t, h, userB, "list_activities", `{}`); r.Text != msgNotConnected {
		t.Errorf("unconnected user: %q", r.Text)
	}
}

// get_activity attaches the streams file of an activity from the last 14
// days unasked; an older one costs no streams read.
func TestGetActivity_recentStreams(t *testing.T) {
	f := newFakeStrava(t) // now: 2026-11-03 10:02 UTC
	f.addAthlete(1, "Anna", "code-a", 250)
	ride := synthRide(false)
	f.addActivity(1, 1001, "Albis loop", "Ride", local(2026, 11, 1, 9), &ride, nil)
	// start_date is the local start an hour earlier: 11:00 UTC is 13d 23h
	// before now, 09:00 UTC 14d 1h.
	f.addActivity(1, 1002, "Edge in", "Ride", local(2026, 10, 20, 12), &ride, nil)
	f.addActivity(1, 1003, "Edge out", "Ride", local(2026, 10, 20, 10), &ride, nil)
	f.addActivity(1, 1004, "Broken", "Ride", local(2026, 11, 2, 9), &ride, nil)
	f.addActivity(1, 1005, "Pool", "Swim", local(2026, 11, 2, 7), nil, nil)
	f.failStreams[1004] = true
	_, h, _ := newTestService(t, f)
	connect(t, h, userA, "code-a")

	r := callTool(t, h, userA, "get_activity", `{"id":1001}`)
	csv := r.file("strava_1001_streams.csv")
	if r.IsError || !strings.HasPrefix(csv, hdrStreams+"\n") || strings.Count(csv, "\n") < 10 {
		t.Fatalf("recent activity without its streams file: %s\n%.200s", r.Text, csv)
	}
	if !strings.Contains(r.Text, "attached for run_python: strava_1001_streams.csv") {
		t.Errorf("file not named:\n%s", r.Text)
	}
	// Stored: a second look reads no streams again.
	callTool(t, h, userA, "get_activity", `{"id":1001}`)
	if n := f.count("/activities/1001/streams"); n != 1 {
		t.Errorf("%d streams reads, want 1", n)
	}

	if r := callTool(t, h, userA, "get_activity", `{"id":1002}`); r.file("strava_1002_streams.csv") == "" {
		t.Errorf("13 days 23 hours old: no streams file:\n%s", r.Text)
	}
	if r := callTool(t, h, userA, "get_activity", `{"id":1003}`); len(r.Files) != 0 || f.count("/activities/1003/streams") != 0 {
		t.Errorf("past 14 days: %d files, %d streams reads", len(r.Files), f.count("/activities/1003/streams"))
	}
	// The streams failing or missing never fails the activity itself.
	for _, id := range []string{"1004", "1005"} {
		r := callTool(t, h, userA, "get_activity", `{"id":`+id+`}`)
		if r.IsError || len(r.Files) != 0 || !strings.Contains(r.Text, "[1] Activity "+id) {
			t.Errorf("activity %s: error %v, %d files:\n%s", id, r.IsError, len(r.Files), r.Text)
		}
	}
	// A manual entry has no streams: none asked for.
	f.addActivity(1, 1006, "Gym", "Workout", local(2026, 11, 2, 18), &ride, map[string]any{"manual": true})
	if r := callTool(t, h, userA, "get_activity", `{"id":1006}`); len(r.Files) != 0 || f.count("/activities/1006/streams") != 0 {
		t.Errorf("manual entry: %d files, %d streams reads", len(r.Files), f.count("/activities/1006/streams"))
	}
	// The same zones as activity_analysis: two files of one name agree.
	if a := callTool(t, h, userA, "activity_analysis", `{"id":1001}`); a.file("strava_1001_streams.csv") != csv {
		t.Error("get_activity and activity_analysis attach different streams files")
	}
	// Past half the 15-minute budget an unasked read stops; kept streams
	// still come at no cost.
	f.addActivity(1, 1007, "Late", "Ride", local(2026, 11, 3, 8), &ride, nil)
	f.mu.Lock()
	f.rateUsed15 = 60
	f.mu.Unlock()
	// 1007's own detail read reports the usage before its streams would go.
	if r := callTool(t, h, userA, "get_activity", `{"id":1007}`); r.IsError || len(r.Files) != 0 || f.count("/activities/1007/streams") != 0 {
		t.Errorf("past the cold budget: error %v, %d files, %d streams reads", r.IsError, len(r.Files), f.count("/activities/1007/streams"))
	}
	if r := callTool(t, h, userA, "get_activity", `{"id":1001}`); r.file("strava_1001_streams.csv") == "" {
		t.Error("kept streams withheld past the cold budget")
	}
}
