package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

// run_bests: the best efforts Strava finds in every run (400 m to the
// marathon, only in an activity's detail, not in the list), their monthly
// progress, a race prediction and the threshold pace.
//
// Predictions use Daniels' VDOT: the oxygen cost of a speed over the share
// of VO2max a race of that length allows. They rest on best efforts within
// runs, not races: a lower bound, a race usually comes out faster.
const (
	defaultBestsMonths = 12
	maxBestsMonths     = 24
	// predictDays: only this recent an effort says what the runner can do
	// now.
	predictDays = 90
	// minVDOTM: shorter efforts (a 400 m or a kilometre) are GPS-noisy and
	// overrate the VDOT.
	minVDOTM = 3000.0
	// vdotConfirm: the best VDOT counts when another run's comes this close.
	// One GPS jump (a tunnel, the watch reacquiring) made a 24-minute 5K
	// runner a 14-minute one: a 2:30 marathon and a threshold that cut every
	// strapless run's load to a quarter.
	vdotConfirm = 3.0
	// tShare is the share of VO2max Daniels' threshold (T) pace runs at.
	tShare = 0.88
	// riegelExp is Riegel's fatigue exponent.
	riegelExp = 1.06
)

// vo2Of is the oxygen cost (ml/kg/min) of running v metres a minute.
func vo2Of(v float64) float64 { return -4.60 + 0.182258*v + 0.000104*v*v }

// shareOf is the share of VO2max a race of tmin minutes holds.
func shareOf(tmin float64) float64 {
	return 0.8 + 0.1894393*math.Exp(-0.012778*tmin) + 0.2989558*math.Exp(-0.1932605*tmin)
}

// vdotOf is Daniels' VDOT of running distM in secs.
func vdotOf(distM, secs float64) float64 {
	if distM <= 0 || secs <= 0 {
		return nan
	}
	t := secs / 60
	return vo2Of(distM/t) / shareOf(t)
}

// timeFor is the time (s) a runner of vdot needs for distM.
func timeFor(vdot, distM float64) float64 {
	lo, hi := distM/600*60, distM/50*60 // 10 m/s to 0.8 m/s
	for range 80 {
		mid := (lo + hi) / 2
		if vdotOf(distM, mid) > vdot {
			lo = mid // too fast for this vdot
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// thresholdSpeed is the T-pace speed (m/s) of vdot.
func thresholdSpeed(vdot float64) float64 {
	vo2 := tShare * vdot
	a, b, c := 0.000104, 0.182258, -4.60-vo2
	v := (-b + math.Sqrt(b*b-4*a*c)) / (2 * a) // m/min
	return v / 60
}

// riegel is the time over d2 of a runner who ran d1 in secs.
func riegel(secs, d1, d2 float64) float64 { return secs * math.Pow(d2/d1, riegelExp) }

// wallClock is t as the stored entries and summaries compare starts: the
// athlete's local wall clock, as a string (the sidecar knows no time zone).
func wallClock(t time.Time) string { return t.Format("2006-01-02T15:04:05") }

// startedSince says x started on or after t, on the wall clock.
func startedSince(x summaryActivity, t time.Time) bool {
	return wallClock(x.localStart()) >= wallClock(t)
}

// bestEffortOf is one best effort and the run it was in.
type bestEffortOf struct {
	name  string
	distM float64
	secs  int
	act   summaryActivity
	vdot  float64
}

// predictTargets are the races predicted.
var predictTargets = []struct {
	name  string
	distM float64
}{{"5K", 5000}, {"10K", 10000}, {"half marathon", 21097.5}, {"marathon", 42195}}

const (
	hdrRunBests       = "month,effort,distance_m,secs,activity_id,date"
	hdrRunPredictions = "race,distance_m,vdot_s,riegel_s,vdot,threshold_s_km"
)

// topVDOT is the effort whose VDOT counts: the highest one another run's
// comes within vdotConfirm of; confirmed false without such a pair (then
// the best of the runner-up run, or of the only run, for the prediction
// alone: an unconfirmed VDOT never weighs a run's load). cands are sorted
// best first.
func topVDOT(cands []bestEffortOf) (*bestEffortOf, bool) {
	if len(cands) == 0 {
		return nil, false
	}
	for i := range cands {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].act.ID == cands[i].act.ID {
				continue
			}
			if cands[i].vdot-cands[j].vdot <= vdotConfirm {
				return &cands[i], true
			}
			break
		}
	}
	for i := range cands {
		if cands[i].act.ID != cands[0].act.ID {
			return &cands[i], false
		}
	}
	return &cands[0], false
}

// vdotCands are the efforts of predictDays before now long enough to weigh,
// best VDOT first.
func vdotCands(efforts []bestEffortOf, now time.Time) []bestEffortOf {
	recent := now.AddDate(0, 0, -predictDays)
	var out []bestEffortOf
	for _, e := range efforts {
		if e.distM >= minVDOTM && startedSince(e.act, recent) && finite(e.vdot) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(p, q bestEffortOf) int { return cmp.Compare(q.vdot, p.vdot) })
	return out
}

// effortsIn are the kept best efforts of every run in f.
func effortsIn(f bestsFile) []bestEffortOf {
	var out []bestEffortOf
	for k, r := range f.Runs {
		id, _ := strconv.ParseInt(k, 10, 64)
		x := summaryActivity{ID: id, StartDateLocal: r.Start}
		for _, e := range r.Efforts {
			out = append(out, bestEffortOf{name: e.Name, distM: e.DistM, secs: e.Secs, act: x, vdot: vdotOf(e.DistM, float64(e.Secs))})
		}
	}
	return out
}

// runThresholdOf is the confirmed best effort of the kept runs of the last
// predictDays: the load's threshold pace, derived from local data each call.
func runThresholdOf(f bestsFile, now time.Time) (bestEffortOf, bool) {
	top, ok := topVDOT(vdotCands(effortsIn(f), now))
	if !ok {
		return bestEffortOf{}, false
	}
	return *top, true
}

// effortsOf is a run's best efforts from its detail.
func effortsOf(d detailedActivity) runEfforts {
	re := runEfforts{Start: d.StartDateLocal}
	for _, e := range d.BestEfforts {
		if e.Distance > 0 && e.ElapsedTime > 0 {
			re.Efforts = append(re.Efforts, effortRec{Name: e.Name, DistM: e.Distance, Secs: e.ElapsedTime})
		}
	}
	return re
}

// loadBests is the user's best-efforts file for athleteID, under the lock.
func (t *tools) loadBests(user string, athleteID int64) bestsFile {
	sl := t.svc.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	f := t.svc.store.loadBests(user)
	if f.Athlete != 0 && f.Athlete != athleteID {
		return bestsFile{Runs: map[string]runEfforts{}}
	}
	return f
}

// addBests merges add into the user's file, unless the user disconnected
// or reconnected to another athlete meanwhile; nothing new, nothing written.
func (t *tools) addBests(user string, athleteID int64, add map[string]runEfforts) {
	if len(add) == 0 {
		return
	}
	sl := t.svc.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	if c, ok := t.svc.conn(user); !ok || (c.AthleteID != 0 && c.AthleteID != athleteID) {
		return
	}
	f := t.svc.store.loadBests(user)
	if f.Athlete != 0 && f.Athlete != athleteID {
		f = bestsFile{Runs: map[string]runEfforts{}}
	}
	changed := f.Athlete != athleteID
	f.Athlete = athleteID
	for k, v := range add {
		if old, ok := f.Runs[k]; !ok || old.Start != v.Start || !slices.Equal(old.Efforts, v.Efforts) {
			f.Runs[k], changed = v, true
		}
	}
	if !changed {
		return
	}
	if err := t.svc.store.saveBests(user, f); err != nil {
		// Only a cost: the details are decoded again next time.
		slog.Error("strava: saving best efforts failed", "err", err)
	}
}

// keepEfforts files an analysed run's best efforts (its detail is read
// anyway), so the threshold pace keeps up without run_bests being asked.
// Settled runs only: Strava matches best efforts after the upload.
func (t *tools) keepEfforts(user string, a athlete, d detailedActivity) {
	if runSport(d.SportType) && !d.Manual && t.svc.settled(user, d.summaryActivity) {
		t.addBests(user, a.ID, map[string]runEfforts{strconv.FormatInt(d.ID, 10): effortsOf(d)})
	}
}

func (t *tools) runBests(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		Months int `json:"months"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("run_bests: " + err.Error())
	}
	if args.Months == 0 {
		args.Months = defaultBestsMonths
	}
	if args.Months < 1 || args.Months > maxBestsMonths {
		return mcpserver.ErrorResult(fmt.Sprintf("months must be 1 to %d", maxBestsMonths))
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	defer t.svc.flushIndex(user)
	now := t.svc.now()
	from := now.AddDate(0, -args.Months, 0)
	// The prediction's days are read whatever months says.
	listFrom := from
	if recent := now.AddDate(0, 0, -predictDays); recent.Before(listFrom) {
		listFrom = recent
	}
	all, more, err := t.svc.listActivities(ctx, user, listFrom.Unix()-dayPadS, 0, maxBackfillPages)
	if err != nil {
		return failure(err)
	}
	var runs []summaryActivity
	for _, x := range all {
		if runSport(x.SportType) && !x.Manual && startedSince(x, listFrom) {
			runs = append(runs, x)
		}
	}
	slices.SortStableFunc(runs, func(p, q summaryActivity) int { return q.localStart().Compare(p.localStart()) })

	// Each run's efforts once: from the bests file, else its detail (kept
	// ones free, the others newest first within the budget).
	bf := t.loadBests(user, a.ID)
	add := map[string]runEfforts{}
	var efforts []bestEffortOf
	winRuns, winUnread, fetched, stopped := 0, 0, 0, ""
	for _, x := range runs {
		inWin := startedSince(x, from)
		if inWin {
			winRuns++
		}
		key := strconv.FormatInt(x.ID, 10)
		re, ok := bf.Runs[key]
		if !ok {
			unread := func() {
				if inWin {
					winUnread++
				}
			}
			if !t.svc.detailKept(user, x.ID) {
				if stopped == "" {
					stopped = t.budgetLeft(ctx, fetched)
				}
				if stopped != "" {
					unread()
					continue
				}
				fetched++
			}
			dctx, cancel := context.WithTimeout(ctx, streamTimeout)
			d, err := t.svc.activity(dctx, user, x.ID, false)
			cancel()
			if err != nil {
				var rl *rateLimitedError
				if errors.As(err, &rl) {
					stopped = reasonRateBudget
				}
				unread()
				continue
			}
			re = effortsOf(d)
			if t.svc.settled(user, x) {
				add[key] = re
			}
		}
		for _, e := range re.Efforts {
			efforts = append(efforts, bestEffortOf{name: e.Name, distM: e.DistM, secs: e.Secs, act: x, vdot: vdotOf(e.DistM, float64(e.Secs))})
		}
	}
	t.addBests(user, a.ID, add)

	var b strings.Builder
	var sources []mcpserver.Source
	cited := map[int64]int{}
	cite := func(x summaryActivity) string {
		i, ok := cited[x.ID]
		if !ok {
			i = len(sources)
			cited[x.ID] = i
			sources = append(sources, activitySource(x))
		}
		return marker(i) + " " + x.localStart().Format("2006-01-02") + " · " + x.Name
	}
	u := unitsOf(a)
	fmt.Fprintf(&b, "best efforts of the user's runs, %s – %s (Strava's best efforts inside each run; %d of %d runs read",
		from.Format("2006-01-02"), now.Format("2006-01-02"), winRuns-winUnread, winRuns)
	if winUnread > 0 {
		reason := stopped
		if reason == "" {
			reason = "Strava errors"
		}
		fmt.Fprintf(&b, "; %d not read yet (%s); a later call reads on", winUnread, reason)
	}
	b.WriteString("):\n")
	if more {
		b.WriteString("  the activity list is not read back to the start yet (rate budget): older runs are missing\n")
	}
	var shown []bestEffortOf
	for _, e := range efforts {
		if startedSince(e.act, from) {
			shown = append(shown, e)
		}
	}
	files := []mcpserver.File{}
	if len(shown) == 0 {
		b.WriteString("  no best efforts found in the window (Strava finds them in runs with GPS or a foot pod)\n")
	} else {
		best := map[string]bestEffortOf{}
		for _, e := range shown {
			if c, ok := best[e.name]; !ok || e.secs < c.secs || (e.secs == c.secs && e.act.localStart().After(c.act.localStart())) {
				best[e.name] = e
			}
		}
		names := make([]string, 0, len(best))
		for n := range best {
			names = append(names, n)
		}
		slices.SortFunc(names, func(p, q string) int { return cmp.Compare(best[p].distM, best[q].distM) })
		b.WriteString("  distance | best | pace | run\n")
		for _, n := range names {
			e := best[n]
			fmt.Fprintf(&b, "  %s | %s | %s | %s\n", n, clock(float64(e.secs)), u.pace(e.distM/float64(e.secs), false), cite(e.act))
		}

		type mk struct{ month, name string }
		monthly := map[mk]bestEffortOf{}
		for _, e := range shown {
			k := mk{e.act.localStart().Format("2006-01"), e.name}
			if c, ok := monthly[k]; !ok || e.secs < c.secs {
				monthly[k] = e
			}
		}
		keys := make([]mk, 0, len(monthly))
		for k := range monthly {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(p, q mk) int {
			if c := cmp.Compare(p.month, q.month); c != 0 {
				return c
			}
			return cmp.Compare(monthly[p].distM, monthly[q].distM)
		})
		csvBests := newCSV(hdrRunBests)
		for _, k := range keys {
			e := monthly[k]
			csvBests.row(k.month, k.name, cell(e.distM, 0), intCell(e.secs), strconv.FormatInt(e.act.ID, 10), e.act.localStart().Format("2006-01-02"))
		}
		files = append(files, csvBests.file("strava_run_bests.csv"))
		lines := 0
		for _, n := range []string{"5K", "10K", "Half-Marathon", "1K"} {
			if lines == 2 {
				break
			}
			var parts []string
			for _, k := range keys {
				if k.name == n {
					parts = append(parts, k.month+" "+clock(float64(monthly[k].secs)))
				}
			}
			if len(parts) >= 2 {
				fmt.Fprintf(&b, "monthly bests, %s: %s\n", n, strings.Join(parts, " · "))
				lines++
			}
		}
	}

	// Prediction and threshold from the best VDOT of the last predictDays.
	cands := vdotCands(efforts, now)
	top, confirmed := topVDOT(cands)
	if top == nil {
		fmt.Fprintf(&b, "no prediction: no best effort of %.0f km or more in the last %d days\n", minVDOTM/1000, predictDays)
	} else {
		tms := thresholdSpeed(top.vdot)
		pred := newCSV(hdrRunPredictions)
		var parts []string
		for _, r := range predictTargets {
			v, rg := timeFor(top.vdot, r.distM), riegel(float64(top.secs), top.distM, r.distM)
			parts = append(parts, fmt.Sprintf("%s %s (Riegel %s)", r.name, clock(v), clock(rg)))
			pred.row(r.name, cell(r.distM, 1), cell(v, 0), cell(rg, 0), cell(top.vdot, 1), cell(1000/tms, 0))
		}
		basis := fmt.Sprintf("confirmed by another run within %g", vdotConfirm)
		if !confirmed {
			basis = fmt.Sprintf("no other run came within %g, so the best of the runner-up run (a single high one may be a GPS jump)", vdotConfirm)
			if cands[0].act.ID == top.act.ID {
				basis = "from one run only: unconfirmed"
			}
		}
		fmt.Fprintf(&b, "prediction (Daniels VDOT %.1f from the %s of %s in %s, the last %d days, %s; Riegel %.2f from the same effort): %s\n",
			top.vdot, top.name, clock(float64(top.secs)), cite(top.act), predictDays, basis, riegelExp, strings.Join(parts, " · "))
		b.WriteString("  from best efforts within runs, not races: a race usually comes out faster\n")
		use := "a run without heart rate gets its load (rTSS) from it while this effort is in the last " + strconv.Itoa(predictDays) + " days"
		switch {
		case !confirmed:
			use = "not used for load: no second run confirms it"
		case more || winUnread > 0:
			use += "; reads still missing may move it"
		}
		fmt.Fprintf(&b, "threshold pace (Daniels T, %.0f %% of VO2max): %s; %s\n", tShare*100, u.pace(tms, false), use)
		files = append(files, pred.file("strava_run_predictions.csv"))
	}
	b.WriteString(fileNames(files))
	if len(shown) > 0 {
		calls := `loomcharts.run_bests("in/strava_run_bests.csv", "out/run_bests.png"`
		if top != nil {
			calls += `, predictions="in/strava_run_predictions.csv"`
		}
		calls += fmt.Sprintf(", unit=%q, lang=L)", u.splitName())
		b.WriteString("chart: before you answer, draw it with run_python, this code exactly, L set to \"de\" when you answer in German, " +
			"else left \"en\": import loomcharts; L = \"en\"; " + calls + "\nThen answer; the chart is shown to the user: refer to it, never link or embed it.\n")
	}
	return mcpserver.Result{Text: b.String(), Sources: sources, Files: files}
}
