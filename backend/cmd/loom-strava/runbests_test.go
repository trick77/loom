package main

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Daniels' published table: VDOT 50 runs 5K in 19:57, 10K in 41:21, the
// half in 1:31:35, the marathon in 3:10:49; T pace 4:15 /km.
func TestDaniels(t *testing.T) {
	if v := vdotOf(5000, 19*60+57); !within(v, 50, 0.1) {
		t.Errorf("VDOT of 5K 19:57 = %.2f, want 50", v)
	}
	for _, c := range []struct {
		dist float64
		want float64
	}{{5000, 19*60 + 57}, {10000, 41*60 + 21}, {21097.5, 3600 + 31*60 + 35}, {42195, 3*3600 + 10*60 + 49}} {
		if got := timeFor(50, c.dist); !within(got, c.want, c.want*0.006) {
			t.Errorf("VDOT 50 over %.0f m: %s, want %s", c.dist, clock(got), clock(c.want))
		}
	}
	if pace := 1000 / thresholdSpeed(50); !within(pace, 255, 3) {
		t.Errorf("T pace at VDOT 50: %s /km, want 4:15", clock(pace))
	}
	if got := riegel(1200, 5000, 10000); !within(got, 1200*math.Pow(2, 1.06), 1e-9) {
		t.Errorf("Riegel %v", got)
	}
	// A run without heart rate gets rTSS from the threshold pace: an hour at
	// it is 100, a metre of climb counting as 10 m flat.
	lc := loadCtx{runMS: 4, lthr: map[string]float64{}}
	if v, why := lc.of(summaryActivity{SportType: "Run", MovingTime: 3600, Distance: 14400}); !within(v, 100, 1e-9) || why != "" {
		t.Errorf("rTSS = %v (%q)", v, why)
	}
	if v, _ := lc.of(summaryActivity{SportType: "TrailRun", MovingTime: 3600, Distance: 13400, TotalElevationGain: 100}); !within(v, 100, 1e-9) {
		t.Errorf("rTSS with climb = %v, want 100", v)
	}

	// One glitched run does not set the VDOT: the best another run confirms.
	run := func(id int64, v float64) bestEffortOf { return bestEffortOf{act: summaryActivity{ID: id}, vdot: v} }
	if top, ok := topVDOT([]bestEffortOf{run(1, 75), run(2, 50), run(3, 49)}); !ok || top.act.ID != 2 {
		t.Errorf("glitch: picked %+v confirmed %v", top, ok)
	}
	if top, ok := topVDOT([]bestEffortOf{run(1, 48), run(1, 47)}); ok || top.act.ID != 1 {
		t.Errorf("one run: %+v %v", top, ok)
	}
	if top, ok := topVDOT([]bestEffortOf{run(1, 60), run(2, 50)}); ok || top.act.ID != 2 {
		t.Errorf("no pair: %+v %v, want the runner-up run", top, ok)
	}
	// The threshold comes from efforts of the last 90 days, confirmed by a
	// second run; older ones no longer count.
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	bf := bestsFile{Runs: map[string]runEfforts{
		"1": {Start: "2026-09-01T07:00:00Z", Efforts: []effortRec{{Name: "5K", DistM: 5000, Secs: 1500}}},
		"2": {Start: "2026-10-01T07:00:00Z", Efforts: []effortRec{{Name: "5K", DistM: 5000, Secs: 1510}}},
	}}
	if top, ok := runThresholdOf(bf, now); !ok || top.act.ID != 1 {
		t.Errorf("threshold: %+v %v", top, ok)
	}
	if _, ok := runThresholdOf(bf, now.AddDate(0, 3, 0)); ok {
		t.Error("a threshold from efforts older than 90 days")
	}
	delete(bf.Runs, "2")
	if _, ok := runThresholdOf(bf, now); ok {
		t.Error("an unconfirmed effort weighs the load")
	}
}

// bestsFixture: Anna's runs over a year with best efforts in their details.
func bestsFixture(t *testing.T) (*fakeStrava, *service, string) {
	t.Helper()
	f := newFakeStrava(t)
	f.addAthlete(1, "Anna", "code-a", 250)
	for i := range 12 {
		day := local(2026, 10, 30, 7).AddDate(0, -i, 0)
		fiveK := 25*60 + i*10 // faster lately
		efforts := []map[string]any{
			{"name": "1k", "distance": 1000.0, "elapsed_time": 280 + i, "pr_rank": nil},
			{"name": "5k", "distance": 5000.0, "elapsed_time": fiveK},
		}
		if i%3 == 0 {
			efforts = append(efforts, map[string]any{"name": "10k", "distance": 10000.0, "elapsed_time": 2*fiveK + 120})
		}
		f.addActivity(1, int64(5000+i), fmt.Sprintf("Run %d", i), "Run", day, nil, map[string]any{"distance": 10500.0,
			"moving_time": 3300, "average_speed": 3.2, "best_efforts": efforts})
	}
	f.addActivity(1, 5100, "Ride", "Ride", local(2026, 10, 29, 9), nil, nil)
	svc, h, dir := newTestService(t, f)
	connect(t, h, userA, "code-a")
	return f, svc, dir
}

func TestRunBests(t *testing.T) {
	f, svc, dir := bestsFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	r := callTool(t, h, userA, "run_bests", `{}`)
	if r.IsError {
		t.Fatal(r.Text)
	}
	for _, want := range []string{
		"best efforts of the user's runs", "12 of 12 runs read", "confirmed by another run within 3",
		"5k | 25:00 | 5:00 /km | [1] 2026-10-30", "10k | 52:00", "1k | 4:40",
		"monthly bests, 5k: ", "prediction (Daniels VDOT", "half marathon", "marathon",
		"best efforts within runs, not races", "threshold pace (Daniels T", "gets its load (rTSS) from it",
		`loomcharts.run_bests("in/strava_run_bests.csv", "out/run_bests_12m_km.png", predictions="in/strava_run_predictions.csv", unit="km", lang=L)`,
	} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("lacks %q:\n%s", want, r.Text)
		}
	}
	if first, _, _ := strings.Cut(r.file("strava_run_bests.csv"), "\n"); first != hdrRunBests {
		t.Errorf("bests header %q", first)
	}
	if first, _, _ := strings.Cut(r.file("strava_run_predictions.csv"), "\n"); first != hdrRunPredictions {
		t.Errorf("predictions header %q", first)
	}
	// The threshold comes from the kept efforts, no Strava read.
	if lc := (&tools{svc: svc}).loadCtxOf(userA, athlete{ID: 1}, nil, nil); lc.runMS <= 0 {
		t.Fatalf("no threshold from the kept efforts: %+v", lc)
	}

	// Kept: a repeat reads nothing from Strava, a restart neither.
	before := len(f.seen)
	if r2 := callTool(t, h, userA, "run_bests", `{}`); r2.IsError || len(f.seen) != before {
		t.Errorf("repeat made %d calls: %v", len(f.seen)-before, f.seen[before:])
	}
	svc2, err := newService(config{clientID: testClientID, clientSecret: testSecret, base: svc.base}, f.srv.Client(), fileStore{dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = f.clock
	before = len(f.seen)
	callTool(t, newServer(config{token: testToken}, svc2, "test"), userA, "run_bests", `{}`)
	for _, c := range f.seen[before:] {
		if strings.Contains(c, "/activities/") {
			t.Errorf("after a restart: %s", c)
		}
	}
	if r := callTool(t, h, userA, "run_bests", `{"months":30}`); !r.IsError {
		t.Error("months 30 accepted")
	}

	// A run without heart rate now counts rTSS.
	f.addActivity(1, 5200, "No strap", "Run", local(2026, 11, 1, 7), nil, map[string]any{"has_heartrate": false,
		"average_heartrate": 0.0, "device_watts": false, "average_watts": 0.0, "weighted_average_watts": 0.0})
	f.advance(10 * time.Minute)
	if a := callTool(t, h, userA, "activity_analysis", `{"id":5200}`); !strings.Contains(a.Text, "this activity: load ") {
		t.Errorf("rTSS:\n%s", a.Text)
	}

	// Two fresh, faster runs confirm each other but are not settled: the
	// load still uses the kept threshold, and run_bests says so.
	for i, secs := range []int{1200, 1210} {
		f.addActivity(1, int64(5300+i), "Fast", "Run", local(2026, 11, 3, 8-i), nil, map[string]any{"distance": 5200.0,
			"best_efforts": []map[string]any{{"name": "5k", "distance": 5000.0, "elapsed_time": secs}}})
	}
	f.advance(10 * time.Minute)
	if r := callTool(t, h, userA, "run_bests", `{}`); !strings.Contains(r.Text, "the load uses 5:") ||
		!strings.Contains(r.Text, "it reads settled runs only") {
		t.Errorf("unsettled threshold:\n%s", r.Text)
	}
	f.advance(2 * time.Hour)
	if r := callTool(t, h, userA, "run_bests", `{}`); !strings.Contains(r.Text, "gets its load (rTSS) from it") {
		t.Errorf("settled threshold:\n%s", r.Text)
	}
}

// A run deleted on Strava leaves the bests file even when the metrics file
// does not read: its efforts would keep setting the threshold pace.
func TestRunBests_deletedRun(t *testing.T) {
	f, svc, dir := bestsFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "run_bests", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	if _, ok := svc.store.loadBests(userA).Runs["5000"]; !ok {
		t.Fatal("run 5000 not kept")
	}
	mp := filepath.Join(dir, metricsDir, userA+".json")
	if err := os.Remove(mp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(mp, 0o700); err != nil { // reads as an error
		t.Fatal(err)
	}
	f.deleteActivity(5000)
	f.advance(11 * time.Minute)
	if r := callTool(t, h, userA, "get_activity", `{"id":5000,"refresh":true}`); !r.IsError {
		t.Fatalf("deleted run: %s", r.Text)
	}
	if _, ok := svc.store.loadBests(userA).Runs["5000"]; ok {
		t.Error("a deleted run's efforts stayed")
	}
}

// A run re-tagged on Strava (here a treadmill run) leaves the threshold:
// the index has it as something else.
func TestRunBests_retagged(t *testing.T) {
	_, svc, _ := bestsFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	if r := callTool(t, h, userA, "run_bests", `{}`); r.IsError {
		t.Fatal(r.Text)
	}
	tl := &tools{svc: svc}
	if _, ok := tl.loadBests(userA, 1).Runs["5000"]; !ok {
		t.Fatal("run 5000 not kept")
	}
	sl := svc.slot(userA)
	sl.dataMu.Lock()
	x := svc.indexLocked(userA, sl)
	a := x.Activities["5000"]
	a.Trainer = true
	x.Activities["5000"] = a
	sl.dataMu.Unlock()
	if _, ok := tl.loadBests(userA, 1).Runs["5000"]; ok {
		t.Error("a treadmill run still sets the threshold")
	}
}

// Under a tight budget the reads stop at half of it and say so; a later
// call reads on.
func TestRunBests_partial(t *testing.T) {
	f, svc, _ := bestsFixture(t)
	h := newServer(config{token: testToken}, svc, "test")
	f.mu.Lock()
	f.rateLimit15 = 16
	f.mu.Unlock()
	r := callTool(t, h, userA, "run_bests", `{}`)
	if r.IsError || !strings.Contains(r.Text, "not read yet (the Strava rate budget); a later call reads on") {
		t.Errorf("partial:\n%s", r.Text)
	}
}
