package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/trick77/toolkit/mcpserver"
)

const (
	hdrWBal      = "elapsed_s,distance_km,watts,wbal_kj"
	hdrGradePace = "grade_pct,rel_speed,pace_s_km,seconds"
	// maxGradeRuns bounds the stored streams one grade profile decodes.
	maxGradeRuns = 40
)

// extras is what the analysis adds past its core: text, files, and the
// chart calls offered on request.
type extras struct {
	text      strings.Builder
	files     []mcpserver.File
	onRequest []string
}

// analysisExtras: heart-rate recovery, critical power and W′ balance for a
// ride with power, critical swim speed for a pool swim, and on request the
// runner's own pace per grade. Only kept data: no Strava read.
func (t *tools) analysisExtras(ctx context.Context, user string, a athlete, d detailedActivity, s series, whole rideMetrics,
	c *comparison, gradeProfile bool, u units) *extras {
	x := &extras{}
	t.writeRecovery(x, s, ftpFor(d.SportType, a.FTP), u)
	if whole.HasWatts && rideSport(d.SportType) {
		t.writeCP(x, d, s, whole, c, u)
	}
	if d.SportType == "Swim" && s.lat == nil && poolLaps(d.Laps) {
		t.writeCSS(ctx, x, user, d, u)
	}
	if gradeProfile && runSport(d.SportType) {
		t.writeGradeProfile(x, user, d, s, c, u)
	}
	return x
}

// dropText is a recovery's drop as the model reads it: a fall of 27 is
// "−27", heart rate still rising a minute later "+4 (still rising)".
func dropText(drop float64) string {
	switch {
	case drop > -0.5 && drop < 0.5:
		return "±0"
	case drop < 0:
		return fmt.Sprintf("+%.0f (still rising)", -drop)
	}
	return fmt.Sprintf("−%.0f", drop)
}

// writeRecovery: the heart-rate drop in the minute after the hardest 5 min
// and after each hard effort, a fitness marker best read against the
// user's own earlier values.
func (t *tools) writeRecovery(x *extras, s series, ftp float64, u units) {
	if s.hr == nil {
		return
	}
	at := func(i int) string {
		if s.dist != nil {
			return u.splitName() + " " + u.distNum(s.dist[i])
		}
		return clock(s.elapsed[i])
	}
	ef := efforts(s, 0, s.n, ftp)
	// The minute after an end, through the last second averaged: an effort
	// starting in it (30/30s, 5 min with 45 s rest) leaves no recovery to
	// read.
	busy := func(end int) bool {
		for _, e := range ef {
			if e.A > end && e.A <= end+hrrS+hrrAvgS {
				return true
			}
		}
		return false
	}
	var parts []string
	h5 := hardest5(s)
	if h5 >= 0 && !busy(h5) {
		if r := hrRecoveryAt(s, h5); finite(r.Drop) {
			parts = append(parts, fmt.Sprintf("after the hardest 5 min (%s) %s → %s, %s", at(h5), bpm(r.From), bpm(r.To), dropText(r.Drop)))
		}
	}
	for i, e := range ef {
		if i == maxIntervals {
			break
		}
		end := effortEnd(s, e, ftp)
		if end == h5 || busy(end) {
			continue // the hardest 5 min's own, or no rest to read
		}
		if r := hrRecoveryAt(s, end); finite(r.Drop) {
			parts = append(parts, fmt.Sprintf("after effort %d (%s) %s", i+1, at(end), dropText(r.Drop)))
		}
	}
	if len(parts) == 0 {
		return
	}
	x.text.WriteString("heart-rate recovery (drop in the minute after; more is fitter, read against the user's own, not a norm): " +
		strings.Join(parts, " · ") + "\n")
}

// effortEnd is the last second of e the power itself was above the
// threshold: efforts() ends on a 10 s rolling mean, up to 9 s late, and the
// minute after would start mid-recovery.
func effortEnd(s series, e effort, ftp float64) int {
	thr := effortFTP * ftp
	for i := e.B - 1; i > e.A; i-- {
		if s.watts[i] > thr {
			return i
		}
	}
	return e.B - 1
}

// writeCP fits critical power and W′ to the 2–20 min bests of this ride and
// its window, and runs W′ balance over the ride.
func (t *tools) writeCP(x *extras, d detailedActivity, s series, whole rideMetrics, c *comparison, u units) {
	best := map[int]float64{}
	for _, dur := range cpDurations {
		v := whole.MeanMax[slices.Index(curveDurations, dur)]
		if c != nil {
			for _, e := range c.entries {
				if w := e.val("mm_"+strconv.Itoa(dur), loadCtx{}); finite(w) && (!finite(v) || w > v) {
					v = w
				}
			}
		}
		if finite(v) {
			best[dur] = v
		}
	}
	cp, wp, ok := criticalPower(best)
	if !ok || s.watts == nil {
		return
	}
	bal := wBalance(s.watts, s.skipped, cp, wp)
	lo, at := wp, 0
	for i, v := range bal {
		if v < lo {
			lo, at = v, i
		}
	}
	where := clock(s.elapsed[at])
	if s.dist != nil {
		where = u.splitName() + " " + u.distNum(s.dist[at]) + " (" + where + ")"
	}
	fmt.Fprintf(&x.text, "critical power (two-parameter fit to the best 2, 5, 10 and 20 min of this ride and the window): CP %s · "+
		"W′ %.1f kJ; W′ balance lowest %.1f kJ (%.0f %% left) at %s: below 0 the fit, not the rider, is off\n",
		watts(cp), wp/1000, lo/1000, lo/wp*100, where)
	t2 := newCSV(hdrWBal)
	next := 0.0
	for i := range s.n {
		if s.elapsed[i] < next {
			continue
		}
		next = s.elapsed[i] + streamStepS
		dist := ""
		if s.dist != nil {
			dist = cell(s.dist[i]/1000, 3)
		}
		t2.row(cell(s.elapsed[i], 0), dist, cell(s.watts[i], 0), cell(bal[i]/1000, 2))
	}
	name := fmt.Sprintf("strava_%d_wbal.csv", d.ID)
	x.files = append(x.files, t2.file(name))
	x.onRequest = append(x.onRequest, fmt.Sprintf(`loomcharts.wbal("in/%s", "out/wbal_%d.png", cp=%.0f, w_prime=%.0f, lang=L)`, name, d.ID, cp, wp))
}

// writeCSS: the critical swim speed from the best 400 and 200 laps of the
// kept pool swims of the predictDays up to this one (an old swim is read
// against its own time, not today's).
func (t *tools) writeCSS(ctx context.Context, x *extras, user string, d detailedActivity, u units) {
	local := d.localStart()
	from := local.AddDate(0, 0, -predictDays)
	list, _, err := t.svc.listActivities(ctx, user, from.Unix()-dayPadS, d.utcStart().Unix()+1, 0)
	if err != nil {
		return
	}
	laps := append([]lap(nil), d.Laps...)
	n := 1
	for _, y := range list {
		if y.SportType != "Swim" || y.ID == d.ID || !startedSince(y, from) || y.localStart().After(local) || !t.svc.detailKept(user, y.ID) {
			continue
		}
		dd, err := t.svc.activity(ctx, user, y.ID, false)
		if err != nil || !poolLaps(dd.Laps) {
			continue
		}
		laps = append(laps, dd.Laps...)
		n++
	}
	ms, ok := cssOf(laps)
	if !ok {
		fmt.Fprintf(&x.text, "critical swim speed: needs a 400 and a 200 lap (m or yd) in the pool swims of the %d days up to this one (%d read)\n",
			predictDays, n)
		return
	}
	fmt.Fprintf(&x.text, "critical swim speed (CSS, from the best 400 and 200 laps of %d pool swims in the %d days up to this one): %s; "+
		"about the pace held for 30 min\n", n, predictDays, u.pace(ms, true))
}

// writeGradeProfile: the runner's own speed per grade from the window's
// kept outdoor streams (no read, past the memory cache), and this run's
// pace adjusted by it.
func (t *tools) writeGradeProfile(x *extras, user string, d detailedActivity, s series, c *comparison, u units) {
	// Every outdoor run of the window, trail runs too (the hilly ones a
	// profile needs most; the baseline keeps to the one sport_type). A
	// treadmill's grade is flat 0 at a foot pod's speed: it would set the
	// flat reference every grade is read against, this run's included.
	gp := &gradeProfile{}
	runs := 0
	if !d.indoor() {
		gp.add(s)
		runs++
	}
	if c != nil {
		for _, y := range c.all {
			if runs > maxGradeRuns {
				break
			}
			if !runSport(y.SportType) || y.indoor() || y.ID == d.ID {
				continue
			}
			if es, ok := t.svc.streamsFromDisk(user, y.ID); ok {
				gp.add(es)
				runs++
			}
		}
	}
	flat := gp.flat()
	if !finite(flat) {
		fmt.Fprintf(&x.text, "own grade profile: not enough flat running in the %d outdoor runs read (their streams kept)\n", runs)
		return
	}
	csv := newCSV(hdrGradePace)
	var parts []string
	for g := -maxGradeBin; g <= maxGradeBin; g++ {
		r := gp.rel(g)
		b, _ := gradeBin(float64(g))
		if !finite(r) {
			continue
		}
		csv.row(strconv.Itoa(g), cell(r, 3), cell(1000/(flat*r), 0), strconv.Itoa(len(gp.speeds[b])))
		if g%5 == 0 && g != 0 {
			parts = append(parts, fmt.Sprintf("%+d %% %s", g, u.pace(flat*r, false)))
		}
	}
	name := fmt.Sprintf("strava_%d_grade_pace.csv", d.ID)
	x.files = append(x.files, csv.file(name))
	fmt.Fprintf(&x.text, "own grade profile (median pace per grade, %d outdoor runs' moving seconds, only their kept streams): flat %s",
		runs, u.pace(flat, false))
	if len(parts) > 0 {
		x.text.WriteString(" · " + strings.Join(parts, " · "))
	}
	x.text.WriteString("\n")
	if v, cov := gp.adjusted(s); finite(v) && !d.indoor() {
		fmt.Fprintf(&x.text, "  this run adjusted by it: %s on the flat, over %.0f %% of its moving time (grades too steep or too rare "+
			"for the profile left out; Strava's grade-adjusted pace models an average runner)\n", u.pace(v, false), cov*100)
	}
	x.onRequest = append(x.onRequest, fmt.Sprintf(`loomcharts.grade_pace("in/%s", "out/grade_pace_%d.png", lang=L)`, name, d.ID))
}
