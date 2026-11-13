package main

import (
	"fmt"
	"math"
	"strings"
)

// Swims: Strava keeps no stroke count, no pool length and no lengths, only
// the device's laps (swim intervals, and 0 m laps for the rests between)
// with their average stroke rate. Strokes are estimated from that rate:
// rate × time. Against the phone app's counted values on one real swim:
// 962 strokes for 988, 11.9 per length for 12.2, SWOLF 44 for 42.
const (
	poolShort = 25
	poolLong  = 50
)

// swimLap is one swim interval; strokes and SWOLF NaN without a stroke rate.
type swimLap struct {
	Dist                float64
	Moving, RestAfter   int
	Rate                float64 // strokes per minute
	Lengths             int
	Strokes, SPL, SWOLF float64
}

type swimFigures struct {
	Pool        int
	PoolAssumed bool
	PoolGiven   bool // pool_m: the user said
	Yards       bool // a 25 yd pool (Strava keeps its laps in metres)
	Open        bool // open water: no lengths, no SWOLF
	Laps        []swimLap
	Dist        float64
	Lengths     int
	RestS       int
	// Over the laps with a stroke rate.
	Strokes, SPL, DPS, SWOLF float64
}

// wholeLengths says d (m) is a whole number of lengths (m), within 1 m.
func wholeLengths(d, length float64) bool { return math.Abs(d-length*math.Round(d/length)) <= 1 }

// poolM is the pool's length in metres.
func (f swimFigures) poolM() float64 {
	if f.Yards {
		return float64(f.Pool) * yard
	}
	return float64(f.Pool)
}

// poolUnit is the pool length's unit.
func (f swimFigures) poolUnit() string {
	if f.Yards {
		return "yd"
	}
	return "m"
}

// computeSwim reads d's laps; pool is the user's pool length (25, 50) or 0
// to find it: laps that are whole 25 yd lengths but not 25 m ones were swum
// in a 25 yd pool; a lap not a multiple of 50 m in a 25 m pool; with every
// lap a multiple of 50 it is 25 m, assumed. A GPS track is open water.
// ok false: not a swim.
func computeSwim(d detailedActivity, s series, pool int) (swimFigures, bool) {
	if d.SportType != "Swim" {
		return swimFigures{}, false
	}
	f := swimFigures{Strokes: nan, SPL: nan, DPS: nan, SWOLF: nan, Dist: d.Distance}
	laps := d.Laps
	if len(laps) == 0 {
		laps = []lap{{Distance: d.Distance, MovingTime: d.MovingTime, ElapsedTime: d.ElapsedTime, AverageCadence: d.AverageCadence}}
	}
	if s.lat != nil {
		f.Open = true
		if d.AverageCadence > 0 && d.MovingTime > 0 {
			f.Strokes = d.AverageCadence * float64(d.MovingTime) / 60
			f.DPS = d.Distance / f.Strokes
		}
		return f, true
	}
	switch pool {
	case poolShort, poolLong:
		f.Pool, f.PoolGiven = pool, true
	default:
		f.Pool, f.PoolAssumed = poolShort, true
		allYd, someNotM := true, false
		for _, l := range laps {
			if l.Distance < poolShort/2 {
				continue
			}
			f.PoolAssumed = f.PoolAssumed && wholeLengths(l.Distance, poolLong)
			someNotM = someNotM || !wholeLengths(l.Distance, poolShort)
			allYd = allYd && wholeLengths(l.Distance, poolShort*yard)
		}
		if allYd && someNotM {
			f.Yards, f.PoolAssumed = true, false
		}
	}
	poolM := f.poolM()
	var strokes, lengthsC, movingC, distC float64
	for _, l := range laps {
		if l.Distance < poolM/2 {
			// A rest lap: its time belongs to the interval before it.
			if n := len(f.Laps); n > 0 {
				f.Laps[n-1].RestAfter += l.ElapsedTime
			}
			f.RestS += l.ElapsedTime
			continue
		}
		sl := swimLap{Dist: l.Distance, Moving: l.MovingTime, Rate: nan, Strokes: nan, SPL: nan, SWOLF: nan}
		sl.Lengths = max(1, int(math.Round(l.Distance/poolM)))
		f.Lengths += sl.Lengths
		if l.AverageCadence > 0 && l.MovingTime > 0 {
			sl.Rate = l.AverageCadence
			sl.Strokes = l.AverageCadence * float64(l.MovingTime) / 60
			sl.SPL = sl.Strokes / float64(sl.Lengths)
			sl.SWOLF = float64(l.MovingTime)/float64(sl.Lengths) + sl.SPL
			strokes += sl.Strokes
			lengthsC += float64(sl.Lengths)
			movingC += float64(l.MovingTime)
			distC += l.Distance
		}
		f.Laps = append(f.Laps, sl)
	}
	if strokes > 0 {
		f.Strokes, f.SPL, f.DPS = strokes, strokes/lengthsC, distC/strokes
		f.SWOLF = movingC/lengthsC + f.SPL
	}
	return f, true
}

// swimDist is a swim distance in metres, or yards for an imperial athlete
// (like the pace per 100 yd).
func swimDist(m float64, u units) string {
	if u.imperial {
		return thousands(m/0.9144) + " yd"
	}
	return thousands(m) + " m"
}

// writeSwimSummary is the swim's tables: efficiency, the swim, intervals.
func writeSwimSummary(b *strings.Builder, f swimFigures, d detailedActivity, u units) {
	b.WriteString("\nswim summary tables (show them as tables in the answer; stroke values are estimated from Strava's average stroke rate " +
		"per lap × time: Strava keeps no stroke count; per stroke style is not in Strava's data):\n")
	var eff []string
	if finite(f.Strokes) {
		eff = append(eff, "strokes ≈ "+thousands(f.Strokes))
	}
	if d.AverageCadence > 0 {
		eff = append(eff, fmt.Sprintf("stroke rate %.0f /min (Strava's, over its moving time)", d.AverageCadence))
	}
	if finite(f.SPL) {
		eff = append(eff, "strokes per length ≈ "+num(f.SPL, 1))
	}
	if finite(f.DPS) {
		eff = append(eff, "distance per stroke ≈ "+num(f.DPS, 2)+" m")
	}
	if finite(f.SWOLF) {
		eff = append(eff, "SWOLF ≈ "+num(f.SWOLF, 0)+" (s + strokes per length)")
	}
	if len(eff) > 0 {
		b.WriteString("  efficiency | " + strings.Join(eff, " | ") + "\n")
	}
	swim := []string{swimDist(f.Dist, u)}
	if !f.Open {
		swim = append(swim, fmt.Sprintf("%d lengths of %d %s", f.Lengths, f.Pool, f.poolUnit()))
	}
	swim = append(swim, "moving "+clock(float64(d.MovingTime)), "pace "+u.pace(d.AverageSpeed, true),
		fmt.Sprintf("speed %.2f m/s", d.AverageSpeed))
	if f.RestS > 0 {
		swim = append(swim, "rest "+clock(float64(f.RestS)))
	}
	b.WriteString("  swim | " + strings.Join(swim, " | ") + "\n")
	switch {
	case f.Open:
		b.WriteString("  open water (a GPS track): no lengths, so no SWOLF\n")
	case f.PoolGiven:
		fmt.Fprintf(b, "  pool %d m (as the user said)\n", f.Pool)
	case f.Yards:
		fmt.Fprintf(b, "  pool %d yd: every lap is whole 25 yd lengths, not 25 m ones\n", f.Pool)
	case f.PoolAssumed:
		fmt.Fprintf(b, "  pool %d m assumed: every lap is a multiple of 50 m, so the laps cannot tell; for a 50 m pool ask again with pool_m 50 (SWOLF about doubles)\n", f.Pool)
	default:
		fmt.Fprintf(b, "  pool %d m: a lap is not a multiple of 50 m\n", f.Pool)
	}
	if f.Open || len(f.Laps) < 2 {
		return
	}
	b.WriteString("  intervals (swim | time | pace | stroke rate | SWOLF est. | rest after):\n")
	for i, l := range f.Laps {
		if i == maxLaps {
			fmt.Fprintf(b, "  … %d more\n", len(f.Laps)-maxLaps)
			break
		}
		line := fmt.Sprintf("  %d. %s · %s · %s", i+1, swimDist(l.Dist, u), clock(float64(l.Moving)), u.pace(l.Dist/float64(max(1, l.Moving)), true))
		if finite(l.Rate) {
			line += fmt.Sprintf(" · %.0f /min · SWOLF ≈ %.0f", l.Rate, l.SWOLF)
		}
		if l.RestAfter > 0 {
			line += " · rest " + clock(float64(l.RestAfter))
		}
		b.WriteString(line + "\n")
	}
}
