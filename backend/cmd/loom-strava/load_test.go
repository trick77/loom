package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

func within(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// Strava's one FTP is the cycling FTP: a run with running power must never
// be divided by it.
func TestFTPFor(t *testing.T) {
	for sport, want := range map[string]float64{"Ride": 250, "VirtualRide": 250, "GravelRide": 250, "MountainBikeRide": 250,
		"EBikeRide": 250, "Run": 0, "TrailRun": 0, "VirtualRun": 0, "Swim": 0, "Walk": 0, "Hike": 0, "Workout": 0} {
		if got := ftpFor(sport, 250); got != want {
			t.Errorf("ftpFor(%s) = %v, want %v", sport, got, want)
		}
	}
}

func TestHRTSS(t *testing.T) {
	if got := hrTSS(3600, 160, 160); !within(got, 100, 1e-9) {
		t.Errorf("an hour at threshold = %v, want 100", got)
	}
	if got := hrTSS(1800, 144, 160); !within(got, 0.5*0.81*100, 1e-9) {
		t.Errorf("half an hour at 90 %% = %v, want 40.5", got)
	}
	for _, bad := range []float64{hrTSS(3600, 0, 160), hrTSS(3600, 150, 0), hrTSS(0, 150, 160)} {
		if !math.IsNaN(bad) {
			t.Errorf("unknown input gave %v, want NaN", bad)
		}
	}
}

func TestActivityLoad(t *testing.T) {
	lc := loadCtx{ftp: 250, lthr: map[string]float64{"running": 170, "cycling": 160}, stored: map[string]storedMetrics{
		"2": {MovingS: 3600, Vals: map[string]float64{"np": 250}},
	}}
	run := summaryActivity{ID: 1, SportType: "Run", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: 153,
		DeviceWatts: true, AverageWatts: 221, WeightedAverageWatts: 224}
	if v, why := lc.of(run); !within(v, 81, 1e-9) || why != "" {
		t.Errorf("run with running power = %v (%q), want hrTSS 81 against the running LTHR", v, why)
	}
	ride := summaryActivity{ID: 2, SportType: "Ride", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: 150}
	if v, _ := lc.of(ride); !within(v, 100, 1e-9) {
		t.Errorf("ride with stored NP = FTP for an hour: %v, want TSS 100", v)
	}
	noPower := summaryActivity{ID: 3, SportType: "Ride", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: 144}
	if v, _ := lc.of(noPower); !within(v, 81, 1e-9) {
		t.Errorf("ride without power = %v, want hrTSS 81 against the cycling LTHR", v)
	}
	ebike := summaryActivity{ID: 4, SportType: "EBikeRide", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: 120}
	if v, why := lc.of(ebike); !math.IsNaN(v) || why != whySport {
		t.Errorf("e-bike without power = %v (%q): no family, no load", v, why)
	}
	for _, c := range []struct {
		x   summaryActivity
		why string
	}{
		{summaryActivity{SportType: "Swim", MovingTime: 2625, HasHeartrate: true, AverageHeartrate: 130}, whySport},
		{summaryActivity{SportType: "Walk", MovingTime: 2625, HasHeartrate: true, AverageHeartrate: 100}, whySport},
		{summaryActivity{SportType: "Run", MovingTime: 2625}, whyNoHR},
	} {
		if v, why := lc.of(c.x); !math.IsNaN(v) || why != c.why {
			t.Errorf("%s: %v (%q), want NaN (%q)", c.x.SportType, v, why, c.why)
		}
	}
	noLTHR := loadCtx{ftp: 250, lthr: map[string]float64{}}
	if v, why := noLTHR.of(run); !math.IsNaN(v) || why != "no_lthr_running" {
		t.Errorf("run without a running LTHR: %v (%q)", v, why)
	}
}

func day0() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

// act is an hour-long run on day d at hour h, avg HR hr against LTHR 100:
// hrTSS = 100 × (hr/100)².
func act(id int64, d, h int, hr float64) summaryActivity {
	start := day0().AddDate(0, 0, d).Add(time.Duration(h) * time.Hour)
	return summaryActivity{ID: id, SportType: "Run", MovingTime: 3600, HasHeartrate: true, AverageHeartrate: hr,
		StartDateLocal: start.Format("2006-01-02T15:04:05Z"), StartDate: start.Format(time.RFC3339)}
}

func TestLoadSeries_ewma(t *testing.T) {
	lc := loadCtx{lthr: map[string]float64{"running": 100}}
	var acts []summaryActivity
	for d := range 400 {
		acts = append(acts, act(int64(d+1), d, 8, 100)) // 100 every day
	}
	end := day0().AddDate(0, 0, 399)
	r := buildLoad(acts, lc, day0(), end, nil)
	last := r.days[len(r.days)-1]
	if !within(last.CTL, 100, 0.1) || !within(last.ATL, 100, 0.01) || !within(last.Form, 0, 0.1) {
		t.Errorf("a constant 100 a day: CTL %.2f ATL %.2f form %.2f, want all at steady state", last.CTL, last.ATL, last.Form)
	}
	// No spread in the week: monotony and strain say nothing.
	if !math.IsNaN(last.Monotony) || !math.IsNaN(last.Strain) || !within(last.ACWR, 1, 0.01) {
		t.Errorf("monotony %v strain %v acwr %v", last.Monotony, last.Strain, last.ACWR)
	}
	if r.counted != 400 || r.total != 400 {
		t.Errorf("counted %d of %d", r.counted, r.total)
	}

	// One load of 42 on the first day: CTL 1, ATL 6.
	r = buildLoad([]summaryActivity{act(1, 0, 8, math.Sqrt(42)*10)}, lc, day0(), day0().AddDate(0, 0, 10), nil)
	if !within(r.days[0].CTL, 1, 1e-9) || !within(r.days[0].ATL, 6, 1e-9) || !within(r.days[1].CTL, 1-1.0/42, 1e-9) {
		t.Errorf("step: day0 CTL %v ATL %v, day1 CTL %v", r.days[0].CTL, r.days[0].ATL, r.days[1].CTL)
	}
	// CTL under 10: no ACWR (a ratio of two small numbers says nothing).
	if !math.IsNaN(r.days[0].ACWR) {
		t.Errorf("ACWR at CTL 1 = %v, want none", r.days[0].ACWR)
	}
	// Ramp needs a week of history.
	if !math.IsNaN(r.days[3].Ramp) || math.IsNaN(r.days[8].Ramp) {
		t.Errorf("ramp day3 %v day8 %v", r.days[3].Ramp, r.days[8].Ramp)
	}
}

// Before and after: the activity's own day without it, then with it; a
// later activity of the same day is not in either.
func TestLoadSeries_beforeAfter(t *testing.T) {
	lc := loadCtx{lthr: map[string]float64{"running": 100}}
	var acts []summaryActivity
	for d := range 60 {
		acts = append(acts, act(int64(d+1), d, 8, 90))
	}
	this := act(1000, 59, 12, 100)
	later := act(1001, 59, 18, 100)
	acts = append(acts, this, later)
	r := buildLoad(acts, lc, day0(), day0().AddDate(0, 0, 59), &this)
	last := r.days[len(r.days)-1]
	if !within(last.CTL-r.before.CTL, 100.0/42, 1e-9) || !within(last.ATL-r.before.ATL, 100.0/7, 1e-9) {
		t.Errorf("this run's 100 moved CTL by %v and ATL by %v", last.CTL-r.before.CTL, last.ATL-r.before.ATL)
	}
	if !within(r.thisLoad, 100, 1e-9) || r.total != 61 {
		t.Errorf("this load %v, total %d (the later one is out)", r.thisLoad, r.total)
	}
	if !within(last.Load, 81+100, 1e-9) {
		t.Errorf("the day's load = %v, want the morning run and this one", last.Load)
	}
}

func TestLoadText(t *testing.T) {
	lc := loadCtx{lthr: map[string]float64{"running": 100}}
	acts := []summaryActivity{act(1, 0, 8, 90), act(2, 1, 8, 90)}
	acts = append(acts, summaryActivity{ID: 3, SportType: "Swim", MovingTime: 2000, HasHeartrate: true, AverageHeartrate: 120,
		StartDateLocal: "2026-06-02T07:00:00Z", StartDate: "2026-06-02T06:00:00Z"})
	acts = append(acts, summaryActivity{ID: 4, SportType: "Ride", MovingTime: 2000, HasHeartrate: true, AverageHeartrate: 120,
		StartDateLocal: "2026-06-01T17:00:00Z", StartDate: "2026-06-01T16:00:00Z"})
	this := acts[1]
	r := buildLoad(acts, lc, day0(), day0().AddDate(0, 0, 1), &this)
	text := loadText(r)
	for _, want := range []string{"fitness and fatigue", "CTL", "ATL", "form", "this activity: load 81",
		"load from 2 of 4 activities", "Swim 1 (no load for this sport)", "Ride 1 (no cycling threshold heart rate yet)"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	csv := loadCSV(7, r).Text
	if first, _, _ := strings.Cut(csv, "\n"); first != hdrLoad || strings.Count(csv, "\n") != 3 {
		t.Errorf("load csv:\n%s", csv)
	}
}
