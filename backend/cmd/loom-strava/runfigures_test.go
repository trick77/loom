package main

import (
	"math"
	"strings"
	"testing"
)

// synthRun is a 7.5 km run at 1 Hz: the first half at v1, the second at v2
// (m/s), cadence 81 per leg, 220 W with one 388 W and one 83 W second, and
// a 200 m burst at 3 m/s in the first half.
func synthRun(v1, v2 float64) streamSet {
	var tm, dist, vel, w, hr, cad, alt floats
	var moving []bool
	d, t := 0.0, 0.0
	burst := false
	for d < 7500 {
		v := v1
		if d >= 3750 {
			v = v2
		}
		if d >= 1000 && d < 1200 {
			v, burst = 3, true
		}
		p := 220.0
		switch int(t) {
		case 100:
			p = 388
		case 200:
			p = 83
		}
		tm = append(tm, t)
		dist = append(dist, d)
		vel = append(vel, v)
		w = append(w, p)
		hr = append(hr, 146)
		cad = append(cad, 81)
		alt = append(alt, 400)
		moving = append(moving, true)
		d += v
		t++
	}
	_ = burst
	return streamSet{Time: &struct{ Data floats }{tm}, Distance: &struct{ Data floats }{dist}, Velocity: &struct{ Data floats }{vel},
		Watts: &struct{ Data floats }{w}, HR: &struct{ Data floats }{hr}, Cadence: &struct{ Data floats }{cad},
		Altitude: &struct{ Data floats }{alt}, Moving: &struct{ Data []bool }{moving}, Grade: &struct{ Data floats }{make(floats, len(tm))}}
}

func TestRunFigures(t *testing.T) {
	s := buildSeries(synthRun(2.55, 2.5))
	m := computeMetrics(s, 0, s.n, nil, nil)
	d := detailedActivity{summaryActivity: summaryActivity{SportType: "Run", Distance: 7500, MovingTime: s.n, AverageSpeed: 2.52, MaxSpeed: 3.04}}
	d.SplitsMetric = []split{{Distance: 1000, AverageGradeAdjustedSpeed: 2.5}, {Distance: 1000, AverageGradeAdjustedSpeed: 2.4}}
	f := computeRunFigures(d, s, m, 87)
	if !within(f.Half1MS, 2.55, 0.03) || !within(f.Half2MS, 2.5, 0.01) || !within(f.SplitPct, (f.Half1MS-f.Half2MS)/f.Half1MS*100, 1e-9) || f.SplitPct <= 0 {
		t.Errorf("halves %.3f / %.3f split %.2f %%", f.Half1MS, f.Half2MS, f.SplitPct)
	}
	if want := 2000 / (1000/2.5 + 1000/2.4); !within(f.GapMS, want, 1e-9) {
		t.Errorf("GAP %.4f, want the splits' %.4f", f.GapMS, want)
	}
	if !within(f.Best200MS, 3, 0.05) {
		t.Errorf("best 200 m %.3f m/s, want the 3 m/s burst", f.Best200MS)
	}
	if f.MaxW != 388 || f.MinW != 83 || !within(f.AvgW, 220, 1) || !within(f.WKg, f.AvgW/87, 1e-9) {
		t.Errorf("power: avg %.1f max %.0f min %.0f W/kg %.2f", f.AvgW, f.MaxW, f.MinW, f.WKg)
	}
	if !within(f.Effectiveness, m.AvgSpeed/f.WKg, 1e-9) {
		t.Errorf("effectiveness %.3f", f.Effectiveness)
	}
	// Strava's run cadence is per leg: steps per minute are twice it.
	if f.AvgSPM != 162 || f.MaxSPM != 162 || !within(f.Steps, float64(s.n)*162/60, 1) || !within(f.StrideM, m.AvgSpeed/(162.0/60), 1e-9) {
		t.Errorf("dynamics: %v spm, max %v, steps %.0f, stride %.3f", f.AvgSPM, f.MaxSPM, f.Steps, f.StrideM)
	}

	var b strings.Builder
	writeRunSummary(&b, f, units{}, 87)
	for _, want := range []string{"summary tables", "speed |", "1st half", "split +", "pace |", "grade-adjusted", "best 200 m",
		"power |", "W/kg at 87 kg", "running effectiveness", "running dynamics |", "162 spm", "step length", "steps",
		"not in Strava's data: vertical oscillation, ground contact time, flight time"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, b.String())
		}
	}

	// Without power or splits those rows go, nothing is zeroed.
	set := synthRun(2.5, 2.5)
	set.Watts = nil
	s = buildSeries(set)
	m = computeMetrics(s, 0, s.n, nil, nil)
	d.SplitsMetric = nil
	f = computeRunFigures(d, s, m, 87)
	if !math.IsNaN(f.GapMS) || !math.IsNaN(f.AvgW) {
		t.Errorf("gap %v avg W %v, want unknown", f.GapMS, f.AvgW)
	}
	b.Reset()
	writeRunSummary(&b, f, units{}, 87)
	if strings.Contains(b.String(), "power |") || strings.Contains(b.String(), "grade-adjusted") {
		t.Errorf("rows without data:\n%s", b.String())
	}
}
