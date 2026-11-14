package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/trick77/toolkit/mcpserver"
)

// efficiency_trend: aerobic efficiency (EF) of easy activities over the
// months, the plainest answer to "am I getting fitter?". More speed or power
// for the same heart rate is fitness, at no race and no test.
//
// From the summaries alone: speed per bpm for runs (climb counted as
// distance, like rTSS), NP per bpm for rides (stored stream NP where there
// is one, else Strava's weighted power). No stream or detail read, so a
// year of activities costs only the list.
const (
	defaultEffMonths = 12
	maxEffMonths     = 24
	// trendMonths: the last months set against the ones before them.
	trendMonths = 3
	// minTrendN: fewer easy activities in a period and its median is one
	// run's day, not a trend.
	minTrendN = 3
)

const hdrEfficiency = "date,activity_id,sport_type,ef,avg_hr,temp_c,easy"

// efficiencyOf is x's EF from its summary alone, one method for every
// activity (a series mixing stream and summary figures showed their gap as
// a trend): running m/min per bpm, cycling Strava's weighted power per bpm;
// NaN without heart rate (or a power meter, for a ride).
func efficiencyOf(x summaryActivity) float64 {
	if !x.HasHeartrate || x.AverageHeartrate <= 0 {
		return nan
	}
	f, fam := familyOf(x.SportType)
	if !fam {
		return nan
	}
	switch f.key {
	case "running":
		if x.Distance <= 0 || x.MovingTime <= 0 {
			return nan
		}
		return (x.Distance + climbFlatM*max(0, x.TotalElevationGain)) / (float64(x.MovingTime) / 60) / x.AverageHeartrate
	case "cycling":
		if x.DeviceWatts && x.WeightedAverageWatts > 0 {
			return x.WeightedAverageWatts / x.AverageHeartrate
		}
	}
	return nan
}

// easyBelow is the average heart rate an easy activity of family stays
// under: where Z3 starts, in the zones the analysis buckets by (the same
// edges() and hrEdges()): Strava's custom ones win, the user's own replace
// Strava's age-based defaults. Strava's unavailable, the user's own still
// count, said. NaN without any. zones are the user's stored ones, fresh.
func (t *tools) easyBelow(ctx context.Context, user string, zones map[string]storedHRZones, family string) (float64, string) {
	own := zones[family].edges()
	z, err := t.svc.zones(ctx, user)
	if err != nil {
		if own != nil {
			return own[1], "their own zones (hr_zones); Strava's zones could not be read, custom ones there may differ"
		}
		return nan, ""
	}
	strava := hrEdges(z)
	switch {
	case strava != nil && z.HeartRate.CustomZones:
		return strava[1], "their custom Strava zones"
	case own != nil:
		return own[1], "their own zones (hr_zones)"
	case strava != nil:
		return strava[1], "Strava's default zones"
	}
	return nan, ""
}

type effPoint struct {
	x    summaryActivity
	ef   float64
	easy bool
}

func (t *tools) efficiencyTrend(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		Sport  string `json:"sport"`
		Months int    `json:"months"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("efficiency_trend: " + err.Error())
	}
	if args.Sport != "running" && args.Sport != "cycling" {
		return mcpserver.ErrorResult("efficiency_trend: sport must be running or cycling")
	}
	if args.Months == 0 {
		args.Months = defaultEffMonths
	}
	if args.Months < 1 || args.Months > maxEffMonths {
		return mcpserver.ErrorResult(fmt.Sprintf("months must be 1 to %d", maxEffMonths))
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	defer t.svc.flushIndex(user)
	now := t.svc.now()
	from := now.AddDate(0, -args.Months, 0)
	// The comparison always has its two periods, whatever months says.
	cut, cut2 := now.AddDate(0, -trendMonths, 0), now.AddDate(0, -2*trendMonths, 0)
	listFrom := from
	if cut2.Before(listFrom) {
		listFrom = cut2
	}
	all, more, err := t.svc.listActivities(ctx, user, listFrom.Unix()-dayPadS, 0, maxBackfillPages)
	if err != nil {
		return failure(err)
	}
	below, zonesFrom := t.easyBelow(ctx, user, t.loadHRZones(user, a.ID), args.Sport)
	// Indoor ones out: a trainer runs hotter, a treadmill's distance is a
	// foot pod's, and a winter of them made a seasonal swing look like
	// fitness.
	var all6 []effPoint // every point since listFrom: the comparison's two periods
	indoor := 0
	for _, x := range all {
		f, ok := familyOf(x.SportType)
		if !ok || f.key != args.Sport || x.Manual || !startedSince(x, listFrom) {
			continue
		}
		if x.indoor() {
			if startedSince(x, from) {
				indoor++
			}
			continue
		}
		if ef := efficiencyOf(x); finite(ef) {
			all6 = append(all6, effPoint{x: x, ef: ef, easy: !finite(below) || x.AverageHeartrate < below})
		}
	}
	slices.SortStableFunc(all6, func(p, q effPoint) int { return p.x.localStart().Compare(q.x.localStart()) })
	var window []effPoint // the points of the months asked for
	for _, p := range all6 {
		if startedSince(p.x, from) {
			window = append(window, p)
		}
	}

	noun := "runs"
	unit := fmt.Sprintf("m/min per bpm: speed with each metre of climb counted as %g m flat, so hills do not read as unfit; "+
		"activity_analysis' EF counts no climb", climbFlatM)
	if args.Sport == "cycling" {
		noun, unit = "rides", "W per bpm: Strava's weighted power per heart beat, for every ride the same way"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "aerobic efficiency of the user's outdoor %s, %s – %s (%s; higher is more for the same heart rate): ",
		noun, from.Format("2006-01-02"), now.Format("2006-01-02"), unit)
	notes := func() {
		if indoor > 0 {
			fmt.Fprintf(&b, "  %d indoor in the window left out (trainer, treadmill, virtual)\n", indoor)
		}
		if more {
			b.WriteString("  the activity list is not read back to the start yet (rate budget): older ones are missing; ask again later\n")
		}
	}
	if len(window) == 0 {
		need := "heart rate"
		if args.Sport == "cycling" {
			need = "heart rate and a power meter"
		}
		fmt.Fprintf(&b, "no outdoor %s activities with %s in the window\n", args.Sport, need)
		notes()
		return mcpserver.Result{Text: b.String()}
	}
	easy := 0
	for _, p := range window {
		if p.easy {
			easy++
		}
	}
	if finite(below) {
		fmt.Fprintf(&b, "%d with heart rate, %d easy (average heart rate below %.0f bpm, where Z3 starts in %s): the trend reads the easy ones, "+
			"a hard session's efficiency says more about the day than the fitness\n", len(window), easy, below, zonesFrom)
	} else {
		fmt.Fprintf(&b, "%d with heart rate; no zones known, so every one counts as easy\n", len(window))
	}
	notes()

	// Monthly medians of the easy ones, and the last months against the
	// ones before.
	type month struct {
		key string
		efs []float64
	}
	var months []month
	for _, p := range window {
		if !p.easy {
			continue
		}
		k := p.x.localStart().Format("2006-01")
		if n := len(months); n == 0 || months[n-1].key != k {
			months = append(months, month{key: k})
		}
		months[len(months)-1].efs = append(months[len(months)-1].efs, p.ef)
	}
	parts := make([]string, 0, len(months))
	for _, m := range months {
		parts = append(parts, fmt.Sprintf("%s %s (%d)", m.key, num(median(m.efs), 2), len(m.efs)))
	}
	b.WriteString("  monthly median, easy only: " + strings.Join(parts, " · ") + "\n")
	var last, prev []float64
	for _, p := range all6 {
		switch {
		case !p.easy:
		case startedSince(p.x, cut):
			last = append(last, p.ef)
		case startedSince(p.x, cut2):
			prev = append(prev, p.ef)
		}
	}
	if ml, mp := median(last), median(prev); len(last) >= minTrendN && len(prev) >= minTrendN && mp > 0 {
		fmt.Fprintf(&b, "  last %d months: %s against %s in the %d months before: %s %% (n %d and %d)\n", trendMonths, num(ml, 2), num(mp, 2),
			trendMonths, signedNum((ml-mp)/mp*100, 1), len(last), len(prev))
	} else {
		fmt.Fprintf(&b, "  last %d months against the %d before: fewer than %d easy activities in one of them (%d and %d), no trend\n",
			trendMonths, trendMonths, minTrendN, len(last), len(prev))
	}
	b.WriteString("  heat, hills and a hard day before all raise heart rate: one point says little, the monthly median more\n")

	csv := newCSV(hdrEfficiency)
	// The kept weather temperature, else the device's; no lookup here.
	tc := t.tempsOf(user, a.ID, 0)
	for _, p := range window {
		temp := tc.shown(p.x)
		csv.row(p.x.localStart().Format("2006-01-02"), strconv.FormatInt(p.x.ID, 10), p.x.SportType, cell(p.ef, 3),
			cell(p.x.AverageHeartrate, 0), cell(temp, 0), boolCell(p.easy))
	}
	files := []mcpserver.File{csv.file("strava_efficiency.csv")}
	b.WriteString(fileNames(files))
	b.WriteString(chartFirst + fmt.Sprintf(`loomcharts.efficiency_trend("in/strava_efficiency.csv", "out/efficiency_trend_%s_%dm.png", sport=%q, lang=L)`,
		args.Sport, args.Months, args.Sport) + "\n" + chartFirstEnd)
	return mcpserver.Result{Text: b.String(), Files: files}
}
