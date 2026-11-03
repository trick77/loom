package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// units renders values in the athlete's measurement preference. Only the
// tool text converts: the CSV files stay metric, their columns are fixed.
type units struct{ imperial bool }

func unitsOf(a athlete) units { return units{imperial: a.MeasurementPreference == "feet"} }

const (
	metersPerMile = 1609.344
	feetPerMeter  = 3.28084
)

// splitM is the per-km (per-mile) bucket.
func (u units) splitM() float64 {
	if u.imperial {
		return metersPerMile
	}
	return 1000
}

func (u units) splitName() string {
	if u.imperial {
		return "mi"
	}
	return "km"
}

func (u units) dist(m float64) string {
	if math.IsNaN(m) {
		return "-"
	}
	v, unit := m/1000, "km"
	if u.imperial {
		v, unit = m/metersPerMile, "mi"
	}
	// A bike's lifetime distance reads better without the decimal.
	if v >= 1000 {
		return thousands(v) + " " + unit
	}
	return fmt.Sprintf("%.1f %s", v, unit)
}

// distNum is a distance without its unit, for "km 12.3–14.0".
func (u units) distNum(m float64) string {
	if u.imperial {
		return fmt.Sprintf("%.1f", m/metersPerMile)
	}
	return fmt.Sprintf("%.1f", m/1000)
}

func (u units) elev(m float64) string {
	if math.IsNaN(m) {
		return "-"
	}
	if u.imperial {
		return thousands(m*feetPerMeter) + " ft"
	}
	return thousands(m) + " m"
}

func (u units) speed(ms float64) string {
	if math.IsNaN(ms) {
		return "-"
	}
	if u.imperial {
		return fmt.Sprintf("%.1f mph", ms*3600/metersPerMile)
	}
	return fmt.Sprintf("%.1f km/h", ms*3.6)
}

// pace is time per km (per mile); per 100 m (100 yd) for a swim.
func (u units) pace(ms float64, swim bool) string {
	if math.IsNaN(ms) || ms <= 0 {
		return "-"
	}
	switch {
	case swim && u.imperial:
		return clock(91.44/ms) + " /100yd"
	case swim:
		return clock(100/ms) + " /100m"
	case u.imperial:
		return clock(metersPerMile/ms) + " /mi"
	default:
		return clock(1000/ms) + " /km"
	}
}

// pace sports are timed per distance rather than measured in speed.
func paceSport(sport string) bool {
	switch sport {
	case "Run", "TrailRun", "VirtualRun", "Walk", "Hike", "Swim":
		return true
	}
	return false
}

// tempo is speed or pace, as the sport is usually read.
func (u units) tempo(ms float64, sport string) string {
	if paceSport(sport) {
		return u.pace(ms, sport == "Swim")
	}
	return u.speed(ms)
}

// clock formats seconds as m:ss or h:mm:ss.
func clock(secs float64) string {
	if math.IsNaN(secs) {
		return "-"
	}
	neg := secs < 0
	s := int(math.Round(math.Abs(secs)))
	out := fmt.Sprintf("%d:%02d", s/60, s%60)
	if s >= 3600 {
		out = fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	if neg {
		return "-" + out
	}
	return out
}

// thousands formats a whole number with comma separators.
func thousands(v float64) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func watts(w float64) string {
	if math.IsNaN(w) {
		return "-"
	}
	return fmt.Sprintf("%.0f W", w)
}

func bpm(v float64) string {
	if math.IsNaN(v) || v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f bpm", v)
}

func num(v float64, prec int) string {
	if math.IsNaN(v) {
		return "-"
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}

// day formats a local start as "Sun 2026-11-01 09:12".
func day(t time.Time) string { return t.Format("Mon 2006-01-02 15:04") }

// ordinal: 1st, 2nd, 3rd, 4th …
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// durationName names a curve duration: 5 s, 1 min, 1.5 h.
func durationName(d int) string {
	switch {
	case d < 60:
		return fmt.Sprintf("%d s", d)
	case d < 3600:
		return fmt.Sprintf("%d min", d/60)
	case d%3600 == 0:
		return fmt.Sprintf("%d h", d/3600)
	default:
		return fmt.Sprintf("%.1f h", float64(d)/3600)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
