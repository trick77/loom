package main

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

// Training load: one number per activity, and from the daily sums the
// fitness (CTL) and fatigue (ATL) curves.
//
// A ride with power counts its TSS against FTP; anything else of the
// cycling and running families counts hrTSS against the family's own
// threshold heart rate (hr_zones). Strava's one FTP is the cycling FTP: a
// run with running power divided by it read as a threshold effort, so runs
// never use FTP. Swims, walks, hikes and other sports add no load (the user
// chose so: no threshold to hold them against).
const (
	ctlDays = 42
	atlDays = 7
	// loadHistoryDays is how far back the daily load is summed: about three
	// CTL time constants, so the start at zero barely shows.
	loadHistoryDays = 120
	// minACWRCTL: below this fitness the acute:chronic ratio of two small
	// numbers says nothing.
	minACWRCTL = 10
	acwrHigh   = 1.5
	acwrLow    = 0.8
)

// Why an activity has no load.
const (
	whySport = "sport"
	whyNoHR  = "no_hr"
	whyLTHR  = "no_lthr_" // + family key
)

// rideSport is any bike sport_type: Ride, VirtualRide, GravelRide,
// MountainBikeRide, EBikeRide, EMountainBikeRide.
func rideSport(sport string) bool { return strings.HasSuffix(sport, "Ride") }

// ftpFor is the FTP that applies to sport: Strava's FTP is a cycling FTP.
func ftpFor(sport string, ftp float64) float64 {
	if rideSport(sport) {
		return ftp
	}
	return 0
}

// hrTSS is the load of movingS at avgHR against the threshold heart rate:
// an hour at threshold is 100, like an hour at FTP.
func hrTSS(movingS int, avgHR, lthr float64) float64 {
	if movingS <= 0 || avgHR <= 0 || lthr <= 0 {
		return nan
	}
	r := avgHR / lthr
	return float64(movingS) / 3600 * r * r * 100
}

// loadCtx is what one activity's load needs: FTP, each family's LTHR and
// the stored stream metrics (their NP beats Strava's weighted power).
type loadCtx struct {
	ftp    float64
	lthr   map[string]float64
	stored map[string]storedMetrics
}

// lthrOf is each family's threshold heart rate from the stored zones.
func lthrOf(zones map[string]storedHRZones) map[string]float64 {
	out := map[string]float64{}
	for k, z := range zones {
		if z.LTHR > 0 {
			out[k] = z.LTHR
		}
	}
	return out
}

// of is x's load, or NaN and why not.
func (c loadCtx) of(x summaryActivity) (float64, string) {
	sm, ok := c.stored[strconv.FormatInt(x.ID, 10)]
	return c.load(x, sm, ok)
}

// load is x's load with its stream metrics sm (ok: known).
func (c loadCtx) load(x summaryActivity, sm storedMetrics, ok bool) (float64, string) {
	if ftp := ftpFor(x.SportType, c.ftp); ftp > 0 {
		if v := activityTSS(x, sm, ok, ftp); finite(v) {
			return v, ""
		}
	}
	f, ok := familyOf(x.SportType)
	if !ok {
		return nan, whySport
	}
	if !x.HasHeartrate || x.AverageHeartrate <= 0 {
		return nan, whyNoHR
	}
	l := c.lthr[f.key]
	if l <= 0 {
		return nan, whyLTHR + f.key
	}
	return hrTSS(x.MovingTime, x.AverageHeartrate, l), ""
}

// loadDay is one day of the series; NaN where a value says nothing.
type loadDay struct {
	Day                                                time.Time
	Load, CTL, ATL, Form, ACWR, Ramp, Monotony, Strain float64
}

// leftOut is a sport's activities without a load, and why.
type leftOut struct {
	sport, why string
	n          int
}

// loadReport is the series from..end and what went into it.
type loadReport struct {
	from     time.Time
	days     []loadDay
	before   loadDay // the activity's day without it; zero without one
	hasThis  bool
	thisLoad float64
	thisWhy  string
	counted  int
	total    int
	left     []leftOut
	// partial: the activity list is not read back to from yet.
	partial bool
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// buildLoad sums the loads of acts per local day from..end and runs the
// curves over them, from zero the day before from. With this, only what
// started before it counts besides it, and before is its day without it.
func buildLoad(acts []summaryActivity, lc loadCtx, from, end time.Time, this *summaryActivity) loadReport {
	from, end = dayOf(from), dayOf(end)
	n := int(end.Sub(from).Hours()/24) + 1
	r := loadReport{from: from, thisLoad: nan}
	if n <= 0 {
		return r
	}
	loads := make([]float64, n)
	var thisDay int
	left := map[[2]string]int{}
	for _, x := range acts {
		ls := x.localStart()
		i := int(dayOf(ls).Sub(from).Hours() / 24)
		if i < 0 || i >= n {
			continue
		}
		isThis := this != nil && x.ID == this.ID
		if this != nil && !isThis && !ls.Before(this.localStart()) {
			continue
		}
		r.total++
		v, why := lc.of(x)
		if isThis {
			r.hasThis, r.thisLoad, r.thisWhy, thisDay = true, v, why, i
		}
		if !finite(v) {
			left[[2]string{x.SportType, why}]++
			continue
		}
		r.counted++
		loads[i] += v
	}
	for k, c := range left {
		r.left = append(r.left, leftOut{sport: k[0], why: k[1], n: c})
	}
	slices.SortFunc(r.left, func(a, b leftOut) int {
		if c := cmp.Compare(b.n, a.n); c != 0 {
			return c
		}
		return cmp.Compare(a.sport+a.why, b.sport+b.why)
	})

	r.days = make([]loadDay, n)
	ctl, atl := 0.0, 0.0
	for i := range n {
		prevCTL, prevATL := ctl, atl
		ctl += (loads[i] - ctl) / ctlDays
		atl += (loads[i] - atl) / atlDays
		d := loadDay{Day: from.AddDate(0, 0, i), Load: loads[i], CTL: ctl, ATL: atl, Form: ctl - atl,
			ACWR: nan, Ramp: nan, Monotony: nan, Strain: nan}
		if ctl >= minACWRCTL {
			d.ACWR = atl / ctl
		}
		if i >= 7 {
			d.Ramp = ctl - r.days[i-7].CTL
		}
		if i >= 6 {
			d.Monotony, d.Strain = monotony(loads[i-6 : i+1])
		}
		r.days[i] = d
		if r.hasThis && i == thisDay {
			without := loads[i]
			if finite(r.thisLoad) {
				without -= r.thisLoad
			}
			b := loadDay{Day: d.Day, Load: without, ACWR: nan, Ramp: nan, Monotony: nan, Strain: nan}
			b.CTL = prevCTL + (without-prevCTL)/ctlDays
			b.ATL = prevATL + (without-prevATL)/atlDays
			b.Form = b.CTL - b.ATL
			r.before = b
		}
	}
	return r
}

// monotony is Foster's: the week's mean daily load over its spread, and
// strain the week's load times it. NaN without spread (a week of rest, or
// the very same load every day).
func monotony(week []float64) (float64, float64) {
	m := mean(week)
	v := 0.0
	for _, x := range week {
		v += (x - m) * (x - m)
	}
	sd := math.Sqrt(v / float64(len(week)))
	if sd < 1e-9 {
		return nan, nan
	}
	mono := m / sd
	return mono, sum(week) * mono
}

func whyText(why string) string {
	switch {
	case why == whySport:
		return "no load for this sport"
	case why == whyNoHR:
		return "no heart rate or power"
	case strings.HasPrefix(why, whyLTHR):
		return "no " + strings.TrimPrefix(why, whyLTHR) + " threshold heart rate yet"
	}
	return why
}

// signedNum is v with its sign, rounded to prec.
func signedNum(v float64, prec int) string {
	if !finite(v) {
		return "-"
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if v >= 0 {
		s = "+" + s
	}
	return s
}

// loadText is the fitness block for the model: the curves on the last day,
// before and after the activity when there is one, the risk figures and
// what went in.
func loadText(r loadReport) string {
	if len(r.days) == 0 {
		return ""
	}
	last := r.days[len(r.days)-1]
	var b strings.Builder
	fmt.Fprintf(&b, "fitness and fatigue on %s (CTL = %d-day and ATL = %d-day weighted daily load, form = CTL − ATL; every cycling "+
		"and running activity since %s: rides with power TSS against FTP, the rest hrTSS against the sport's threshold heart rate): ",
		last.Day.Format("2006-01-02"), ctlDays, atlDays, r.from.Format("2006-01-02"))
	if r.hasThis {
		fmt.Fprintf(&b, "CTL %s → %s · ATL %s → %s · form %s → %s", num(r.before.CTL, 0), num(last.CTL, 0), num(r.before.ATL, 0),
			num(last.ATL, 0), signedNum(r.before.Form, 0), signedNum(last.Form, 0))
		if finite(r.thisLoad) {
			fmt.Fprintf(&b, " · this activity: load %s", num(r.thisLoad, 0))
		} else {
			b.WriteString(" · this activity: no load (" + whyText(r.thisWhy) + ")")
		}
	} else {
		fmt.Fprintf(&b, "CTL %s · ATL %s · form %s", num(last.CTL, 0), num(last.ATL, 0), signedNum(last.Form, 0))
	}
	b.WriteString("\n")
	var risk []string
	switch {
	case !finite(last.ACWR):
		risk = append(risk, fmt.Sprintf("ACWR (ATL/CTL) not given below CTL %d", minACWRCTL))
	case last.ACWR > acwrHigh:
		risk = append(risk, fmt.Sprintf("ACWR (ATL/CTL) %s, above %.1f: load rose fast; a possible contributor to soreness or injury, never the cause",
			num(last.ACWR, 2), acwrHigh))
	case last.ACWR < acwrLow:
		risk = append(risk, fmt.Sprintf("ACWR (ATL/CTL) %s, below %.1f: less load than usual lately", num(last.ACWR, 2), acwrLow))
	default:
		risk = append(risk, fmt.Sprintf("ACWR (ATL/CTL) %s, within %.1f–%.1f", num(last.ACWR, 2), acwrLow, acwrHigh))
	}
	if finite(last.Ramp) {
		risk = append(risk, "ramp "+signedNum(last.Ramp, 1)+" CTL over 7 days")
	}
	if finite(last.Monotony) {
		risk = append(risk, fmt.Sprintf("monotony %s · strain %s (Foster, last 7 days)", num(last.Monotony, 2), thousands(last.Strain)))
	}
	b.WriteString("  " + strings.Join(risk, " · ") + "\n")
	fmt.Fprintf(&b, "  load from %d of %d activities", r.counted, r.total)
	if len(r.left) > 0 {
		parts := make([]string, 0, len(r.left))
		for _, l := range r.left {
			parts = append(parts, fmt.Sprintf("%s %d (%s)", l.sport, l.n, whyText(l.why)))
		}
		b.WriteString("; left out: " + strings.Join(parts, ", "))
	}
	b.WriteString("\n")
	if r.partial {
		b.WriteString("  partial: the activity list is not read back to the start yet (Strava rate budget): CTL reads low; a later call reads more\n")
	}
	return b.String()
}

// loadFor is the analysed activity's load report, from the index back
// loadHistoryDays. A family whose activities there lack a threshold heart
// rate gets one calculated first (the user chose so): hr_zones' reads,
// within what the budget left; partial, those activities count once a later
// call completes it. stored is the user's metrics as the call knows them,
// done the streams it read already. note says what was calculated ("" when
// nothing was needed); calculated lists the families that got a threshold.
func (t *tools) loadFor(ctx context.Context, user string, a athlete, d detailedActivity, stored map[string]storedMetrics,
	done int) (r loadReport, lc loadCtx, note string, calculated []string) {
	local := d.localStart()
	from := dayOf(local).AddDate(0, 0, -(loadHistoryDays - 1))
	lc = loadCtx{ftp: a.FTP, lthr: lthrOf(t.loadHRZones(user, a.ID)), stored: stored}
	list, more, err := t.svc.listActivities(ctx, user, from.Unix()-dayPadS, 0, maxBackfillPages)
	if err != nil {
		return loadReport{}, lc, "", nil
	}
	if !slices.ContainsFunc(list, func(x summaryActivity) bool { return x.ID == d.ID }) {
		list = append(list, d.summaryActivity)
	}
	var need []hrFamily
	for _, x := range list {
		ls := x.localStart()
		if ls.Before(from) || ls.After(local) {
			continue
		}
		if _, why := lc.of(x); strings.HasPrefix(why, whyLTHR) {
			key := strings.TrimPrefix(why, whyLTHR)
			if !slices.ContainsFunc(need, func(f hrFamily) bool { return f.key == key }) {
				f, _ := familyOf(x.SportType)
				need = append(need, f)
			}
		}
	}
	if len(need) > 0 {
		zr, err := t.calcZones(ctx, user, a, need, true, done)
		if err == nil && zr.ran {
			lc.lthr = lthrOf(zr.zones)
			lc.stored = t.loadMetrics(user, a.ID)
			var missing []string
			for _, f := range need {
				if lc.lthr[f.key] > 0 {
					calculated = append(calculated, f.key)
				} else {
					missing = append(missing, f.key)
				}
			}
			if len(calculated) > 0 {
				note = "threshold heart rate calculated now from the user's streams for " + strings.Join(calculated, " and ") +
					" (hr_zones tells how)"
			}
			if len(missing) > 0 {
				if note != "" {
					note += "; "
				}
				note += "no threshold heart rate for " + strings.Join(missing, " and ") + " yet"
				if zr.read.stoppedBy != "" {
					note += " (reads stopped by " + zr.read.stoppedBy + "; a later call reads more)"
				}
			}
		}
	}
	r = buildLoad(list, lc, from, local, &d.summaryActivity)
	r.partial = more
	return r, lc, note, calculated
}

// hdrLoad is the load file's contract with loomcharts.fitness_fatigue.
// ctl_before and atl_before fill only the analysed activity's day: its
// day's values without it.
const hdrLoad = "date,load,ctl,atl,form,acwr,ramp,monotony,strain,ctl_before,atl_before"

// loadCSV is the daily series; id 0 names it for a window, not an activity.
func loadCSV(id int64, r loadReport) mcpserver.File {
	t := newCSV(hdrLoad)
	for _, d := range r.days {
		cb, ab := "", ""
		if r.hasThis && d.Day.Equal(r.before.Day) {
			cb, ab = cell(r.before.CTL, 1), cell(r.before.ATL, 1)
		}
		t.row(d.Day.Format("2006-01-02"), cell(d.Load, 0), cell(d.CTL, 1), cell(d.ATL, 1), cell(d.Form, 1), cell(d.ACWR, 2),
			cell(d.Ramp, 1), cell(d.Monotony, 2), cell(d.Strain, 0), cb, ab)
	}
	name := "strava_load.csv"
	if id != 0 {
		name = fmt.Sprintf("strava_%d_load.csv", id)
	}
	return t.file(name)
}
