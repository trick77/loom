package main

import (
	"math"
	"sort"
)

// Extras computed at analysis time from what the call reads anyway: heart
// rate recovery, critical power and W' balance, critical swim speed and the
// user's own pace per grade. None stores a key per activity (an entryRev
// bump re-reads every stream).

// hrRecovery is the drop of heart rate in the minute after an effort.
type hrRecovery struct {
	From, To, Drop float64
}

const (
	// hrrS: the classic one-minute heart-rate recovery.
	hrrS = 60
	// hrrAvgS: the heart rate at either end is a short mean, not one
	// sample a strap spike can make.
	hrrAvgS = 5
)

// hrRecoveryAt is the recovery after sample end; Drop NaN when the series
// ends too soon or the recording paused in that minute (the watch stopped,
// so the heart rate after the gap is not the minute after).
func hrRecoveryAt(s series, end int) hrRecovery {
	r := hrRecovery{From: nan, To: nan, Drop: nan}
	if s.hr == nil || end < hrrAvgS || end+hrrS+hrrAvgS-2 >= s.n {
		return r
	}
	lo := end + hrrS - 2
	// Every sample up to the last one averaged: a pause there lets heart
	// rate from after it in.
	for i := end + 1; i < lo+hrrAvgS; i++ {
		if s.skipped[i] > 0 {
			return r
		}
	}
	r.From = mean(s.hr[end-hrrAvgS : end])
	r.To = mean(s.hr[lo : lo+hrrAvgS])
	r.Drop = r.From - r.To
	return r
}

// hardest5 is the end sample of the hardest 5 minutes: by power with a
// power meter, else by heart rate; -1 when shorter.
func hardest5(s series) int {
	v := s.watts
	if v == nil {
		v = s.hr
	}
	if v == nil {
		return -1
	}
	best, at := bestWindow(v, 300)
	if math.IsNaN(best) {
		return -1
	}
	return at + 300 - 1
}

// cpDurations are the stored mean-max durations the critical power is
// fitted on: 2 to 20 minutes, where the two-parameter model holds (a new
// duration would be a new stored key).
var cpDurations = []int{120, 300, 600, 1200}

// criticalPower fits work = CP × t + W′ to the best power per duration;
// ok false with fewer than three durations or an implausible fit.
func criticalPower(best map[int]float64) (cp, wPrime float64, ok bool) {
	var ts, ws []float64
	for _, d := range cpDurations {
		if p, has := best[d]; has && finite(p) && p > 0 {
			ts = append(ts, float64(d))
			ws = append(ws, p*float64(d))
		}
	}
	if len(ts) < 3 {
		return nan, nan, false
	}
	mt, mw := mean(ts), mean(ws)
	var num, den float64
	for i := range ts {
		num += (ts[i] - mt) * (ws[i] - mw)
		den += (ts[i] - mt) * (ts[i] - mt)
	}
	cp = num / den
	wPrime = mw - cp*mt
	// W′ of an amateur is a few to a few tens of kJ; outside, the best
	// efforts were not all-out (a curve of tempo rides).
	if cp <= 0 || wPrime < 2000 || wPrime > 60000 {
		return cp, wPrime, false
	}
	return cp, wPrime, true
}

// wBalance is W′ left after each second of w (Skiba's differential form):
// above CP it drains by the excess, below it refills in proportion to what
// is missing. skipped (the series' unrecorded clock before each sample) is
// rest: a café stop with the watch paused refills it, closed form for zero
// power (bal → W′ with time constant W′/CP).
func wBalance(w, skipped []float64, cp, wPrime float64) []float64 {
	out := make([]float64, len(w))
	bal := wPrime
	for i, p := range w {
		if i < len(skipped) && skipped[i] > 0 {
			bal = wPrime - (wPrime-bal)*math.Exp(-cp*skipped[i]/wPrime)
		}
		if p > cp {
			bal -= p - cp
		} else {
			bal += (cp - p) * (wPrime - bal) / wPrime
		}
		out[i] = bal
	}
	return out
}

// yard is a 25 yd pool's unit, in metres.
const yard = 0.9144

// cssOf is the critical swim speed (m/s) from the best 400 and 200 laps:
// (d400 − d200) / (t400 − t200), the pace a swimmer holds for about
// 30 min. Metres first, else yards (Strava keeps a yard pool's laps in
// metres: 365.8 and 182.9).
func cssOf(laps []lap) (float64, bool) {
	best := func(d float64) int {
		t := 0
		for _, l := range laps {
			if math.Abs(l.Distance-d) <= 2 && l.MovingTime > 0 && (t == 0 || l.MovingTime < t) {
				t = l.MovingTime
			}
		}
		return t
	}
	for _, unit := range []float64{1, yard} {
		t400, t200 := best(400*unit), best(200*unit)
		// A 400 takes 2 to 2.5 times a 200 swum the same way: outside, the
		// two came from different sets (a 200 kick, a 400 pull) and the
		// gap between them is no speed.
		if t200 > 0 && float64(t400) >= cssMinRatio*float64(t200) && float64(t400) <= cssMaxRatio*float64(t200) {
			return 200 * unit / float64(t400-t200), true
		}
	}
	return nan, false
}

const (
	cssMinRatio = 1.95
	cssMaxRatio = 2.6
)

// poolLaps says every swim lap is whole lengths of a 25 m or 25 yd pool:
// open water's GPS laps are not, and a short GPS 400 made a too-fast CSS.
func poolLaps(laps []lap) bool {
	n := 0
	for _, l := range laps {
		if l.Distance < 10 {
			continue
		}
		n++
		if !wholeLengths(l.Distance, poolShort) && !wholeLengths(l.Distance, poolShort*yard) {
			return false
		}
	}
	return n > 0
}

// gradeProfile is a runner's own speed per grade (1 % bins, −15 … +15),
// relative to flat: what Strava's GAP takes from a model, from their runs.
type gradeProfile struct {
	speeds [2*maxGradeBin + 1][]float64
	table  []float64 // rels(), nil until asked or after add
}

const (
	maxGradeBin = 15
	// minBinS: fewer seconds in a bin say nothing about it.
	minBinS = 60
)

// add folds one run's moving, recorded seconds in: a sweep keeps no series alive.
func (gp *gradeProfile) add(s series) {
	if s.speed == nil || s.grade == nil {
		return
	}
	for i := range s.n {
		if !s.moving[i] || s.skipped[i] > 0 || s.speed[i] < 0.5 {
			continue
		}
		if b, ok := gradeBin(s.grade[i]); ok {
			gp.speeds[b] = append(gp.speeds[b], s.speed[i])
		}
	}
	gp.table = nil
}

// rels is every bin's speed relative to flat, worked out once.
func (gp *gradeProfile) rels() []float64 {
	if gp.table != nil {
		return gp.table
	}
	for i := range gp.speeds {
		sort.Float64s(gp.speeds[i])
	}
	gp.table = make([]float64, len(gp.speeds))
	flat := gp.median(maxGradeBin)
	for b := range gp.speeds {
		gp.table[b] = nan
		if finite(flat) && flat > 0 {
			gp.table[b] = gp.median(b) / flat
		}
	}
	return gp.table
}

func gradeBin(g float64) (int, bool) {
	r := int(math.Round(g))
	if r < -maxGradeBin || r > maxGradeBin {
		return 0, false
	}
	return r + maxGradeBin, true
}

// median of bin b (sorted by rels); NaN with fewer than minBinS seconds.
func (gp *gradeProfile) median(b int) float64 {
	v := gp.speeds[b]
	if len(v) < minBinS {
		return nan
	}
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// flat is the median speed on the flat.
func (gp *gradeProfile) flat() float64 {
	gp.rels()
	return gp.median(maxGradeBin)
}

// rel is the speed at grade g relative to flat; NaN without enough of
// either.
func (gp *gradeProfile) rel(g int) float64 {
	b, ok := gradeBin(float64(g))
	if !ok {
		return nan
	}
	return gp.rels()[b]
}

// adjusted is s's own grade-adjusted speed: each second as fast as it
// would have been on the flat, by this profile; covered is the share of its
// moving seconds that had a factor (a grade too steep or too rare has
// none). NaN with no second covered.
func (gp *gradeProfile) adjusted(s series) (speed, covered float64) {
	if s.speed == nil || s.grade == nil {
		return nan, 0
	}
	t := gp.rels()
	sum, n, all := 0.0, 0, 0
	for i := range s.n {
		if !s.moving[i] || s.skipped[i] > 0 || s.speed[i] < 0.5 {
			continue
		}
		all++
		b, ok := gradeBin(s.grade[i])
		if !ok || !finite(t[b]) || t[b] <= 0 {
			continue
		}
		sum += s.speed[i] / t[b]
		n++
	}
	if n == 0 {
		return nan, 0
	}
	return sum / float64(n), float64(n) / float64(all)
}
