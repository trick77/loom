package main

import (
	"fmt"
	"math"
	"strings"
)

// footSport: Strava's cadence for these is per leg (a run at 163 steps a
// minute reads 81.5): steps per minute are twice it.
func footSport(sport string) bool {
	switch sport {
	case "Run", "TrailRun", "VirtualRun", "Walk", "Hike":
		return true
	}
	return false
}

// runSport gets the run summary tables: the running family of hrFamilies.
func runSport(sport string) bool {
	f, ok := familyOf(sport)
	return ok && f.key == "running"
}

// best200M is the stretch "best pace" is measured over: long enough that
// one GPS jump does not make it.
const best200M = 200.0

// runFigures are the run summary's values; NaN where the data lacks them.
type runFigures struct {
	AvgMS, MaxMS, Half1MS, Half2MS, SplitPct, GapMS, Best200MS float64
	AvgW, NP, MaxW, MinW, WKg, Effectiveness, KJ               float64
	AvgSPM, MaxSPM, StrideM, Steps                             float64
	EF, Decoupling                                             float64
}

// computeRunFigures reads one run: Strava's averages and splits, the rest
// from the streams (moving samples, gaps left out).
func computeRunFigures(d detailedActivity, s series, m rideMetrics, weight float64) runFigures {
	f := runFigures{AvgMS: positive(d.AverageSpeed), MaxMS: positive(d.MaxSpeed), Half1MS: nan, Half2MS: nan, SplitPct: nan,
		GapMS: nan, Best200MS: nan, AvgW: nan, NP: nan, MaxW: nan, MinW: nan, WKg: nan, Effectiveness: nan, KJ: nan,
		AvgSPM: nan, MaxSPM: nan, StrideM: nan, Steps: nan, EF: m.EF, Decoupling: m.Decoupling}

	// Grade-adjusted: Strava's per split, over the whole distance.
	dist, secs := 0.0, 0.0
	for _, sp := range d.SplitsMetric {
		if sp.Distance > 0 && sp.AverageGradeAdjustedSpeed > 0 {
			dist += sp.Distance
			secs += sp.Distance / sp.AverageGradeAdjustedSpeed
		}
	}
	if secs > 0 {
		f.GapMS = dist / secs
	}

	if s.dist != nil && s.n > 1 {
		half := (s.dist[0] + s.dist[s.n-1]) / 2
		var d1, t1, d2, t2 float64
		for i := 1; i < s.n; i++ {
			if !s.moving[i] || s.skipped[i] > 0 {
				continue
			}
			dd, dt := s.dist[i]-s.dist[i-1], s.elapsed[i]-s.elapsed[i-1]
			if s.dist[i] <= half {
				d1, t1 = d1+dd, t1+dt
			} else {
				d2, t2 = d2+dd, t2+dt
			}
		}
		if t1 > 0 && t2 > 0 {
			f.Half1MS, f.Half2MS = d1/t1, d2/t2
			f.SplitPct = (f.Half1MS - f.Half2MS) / f.Half1MS * 100
		}
		f.Best200MS = bestOver(s, best200M)
	}

	if m.HasWatts {
		f.AvgW, f.NP, f.MaxW, f.KJ = m.AvgW, m.NP, m.MaxW, m.KJ
		for i := range s.n {
			if s.moving[i] && s.watts[i] > 0 && (math.IsNaN(f.MinW) || s.watts[i] < f.MinW) {
				f.MinW = s.watts[i]
			}
		}
		if weight > 0 {
			f.WKg = f.AvgW / weight
			if finite(m.AvgSpeed) && f.WKg > 0 {
				f.Effectiveness = m.AvgSpeed / f.WKg
			}
		}
	}

	if finite(m.AvgCad) && m.AvgCad > 0 {
		f.AvgSPM, f.MaxSPM = m.AvgCad*2, m.MaxCad*2
		if finite(m.AvgSpeed) {
			f.StrideM = m.AvgSpeed / (f.AvgSPM / 60)
		}
		steps := 0.0
		for i := range s.n {
			if s.moving[i] {
				steps += s.cad[i] * 2 / 60
			}
		}
		f.Steps = steps
	}
	return f
}

// bestOver is the highest speed over any stretch of at least m metres with
// no recording gap in it; NaN when the activity is shorter.
func bestOver(s series, m float64) float64 {
	best := nan
	gapsUpTo := make([]int, s.n) // gaps among samples 1..i
	for i := 1; i < s.n; i++ {
		gapsUpTo[i] = gapsUpTo[i-1]
		if s.skipped[i] > 0 {
			gapsUpTo[i]++
		}
	}
	i := 0
	for j := 1; j < s.n; j++ {
		for i+1 < j && s.dist[j]-s.dist[i+1] >= m {
			i++
		}
		if s.dist[j]-s.dist[i] < m || gapsUpTo[j] != gapsUpTo[i] {
			continue
		}
		if dt := s.elapsed[j] - s.elapsed[i]; dt > 0 {
			if v := (s.dist[j] - s.dist[i]) / dt; math.IsNaN(best) || v > best {
				best = v
			}
		}
	}
	return best
}

// writeRunSummary is the run's figures as table rows, the answer's tables
// (speed, pace, power, running dynamics); a row without data is left out.
func writeRunSummary(b *strings.Builder, f runFigures, u units, weight float64) {
	type item struct {
		v    float64
		text string
	}
	row := func(name string, items ...item) {
		var parts []string
		for _, it := range items {
			if finite(it.v) {
				parts = append(parts, it.text)
			}
		}
		if len(parts) > 0 {
			b.WriteString("  " + name + " | " + strings.Join(parts, " | ") + "\n")
		}
	}
	pace := func(ms float64) string { return u.pace(ms, false) }
	b.WriteString("\nsummary tables (show them as tables in the answer):\n")
	split := ""
	if finite(f.SplitPct) {
		kind := "positive: the 2nd half slower"
		if f.SplitPct < 0 {
			kind = "negative: the 2nd half faster"
		}
		split = fmt.Sprintf("split %s %% (%s)", signedNum(f.SplitPct, 1), kind)
	}
	row("speed", item{f.AvgMS, "avg " + u.speed(f.AvgMS)}, item{f.MaxMS, "max " + u.speed(f.MaxMS)},
		item{f.Half1MS, "1st half " + u.speed(f.Half1MS)}, item{f.Half2MS, "2nd half " + u.speed(f.Half2MS)}, item{f.SplitPct, split})
	row("pace", item{f.AvgMS, "avg " + pace(f.AvgMS)}, item{f.GapMS, "grade-adjusted " + pace(f.GapMS) + " (Strava's splits)"},
		item{f.Best200MS, fmt.Sprintf("best %.0f m %s", best200M, pace(f.Best200MS))},
		item{f.Half1MS, "1st half " + pace(f.Half1MS)}, item{f.Half2MS, "2nd half " + pace(f.Half2MS)})
	if finite(f.AvgW) {
		row("power", item{f.AvgW, "avg " + watts(f.AvgW)}, item{f.NP, "weighted (NP) " + watts(f.NP)}, item{f.MaxW, "max " + watts(f.MaxW)},
			item{f.MinW, "min " + watts(f.MinW)}, item{f.WKg, fmt.Sprintf("%.2f W/kg at %.0f kg", f.WKg, weight)},
			item{f.Effectiveness, "running effectiveness " + num(f.Effectiveness, 2) + " (m/s per W/kg)"},
			item{f.KJ, thousands(f.KJ) + " kJ"}, item{f.Decoupling, "aerobic decoupling " + num(f.Decoupling, 1) + " %"},
			item{f.EF, "aerobic efficiency (NP/HR) " + num(f.EF, 2)})
	} else {
		row("heart rate", item{f.Decoupling, "aerobic decoupling " + num(f.Decoupling, 1) + " %"},
			item{f.EF, "aerobic efficiency (speed m/min per bpm) " + num(f.EF, 2)})
	}
	// One footfall, not a stride (two steps): what the phone app calls
	// Schrittlänge.
	stride := fmt.Sprintf("step length %.0f cm", f.StrideM*100)
	if u.imperial {
		stride = fmt.Sprintf("step length %.0f in", f.StrideM*100/2.54)
	}
	row("running dynamics", item{f.AvgSPM, fmt.Sprintf("cadence avg %.0f spm", f.AvgSPM)}, item{f.MaxSPM, fmt.Sprintf("max %.0f spm", f.MaxSPM)},
		item{f.StrideM, stride}, item{f.Steps, "steps " + thousands(f.Steps)})
	b.WriteString("  not in Strava's data: vertical oscillation, ground contact time, flight time, vertical and flight ratio\n")
}
