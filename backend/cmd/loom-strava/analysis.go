package main

import (
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

	"github.com/trick77/loom/internal/mcpserver"
)

const (
	defaultWeeks = 6
	maxWeeks     = 26
	maxCompare   = 5
	// coldBudget is the share of the 15-minute read budget a cold baseline
	// may spend on streams; the next call reads the rest.
	coldBudget = 0.5
	// maxColdFetches bounds the stream reads of one call.
	maxColdFetches = 40
	// minTimeLeft stops the baseline reads while the tool can still answer.
	minTimeLeft = 6 * time.Second
	// streamTimeout bounds one baseline stream read: one slow answer must not
	// eat the whole tool deadline.
	streamTimeout = 8 * time.Second
	// sameRouteM and sameRouteDist: same start and end within 200 m, the
	// distance within 5 %.
	sameRouteM    = 200.0
	sameRouteDist = 0.05
	maxSameRoute  = 3
	gapStepM      = 200.0
	maxIntervals  = 15
	// minFlagN: with fewer activities "best in window" says nothing.
	minFlagN = 3
)

// metric kinds decide the words of a flag and which way is better.
const (
	kindVolume  = iota // more is more, not better: highest/lowest
	kindPerf           // higher is better: best/worst
	kindLower          // lower is better (HR drift): best/worst
	kindNeutral        // no flag (avg HR)
)

// shownDurations are the mean-max durations named in the text.
var shownDurations = []int{5, 30, 60, 300, 600, 1200, 3600}

// baseEntry is one activity of the comparison window.
type baseEntry struct {
	act       summaryActivity
	sm        storedMetrics
	known     bool // sm holds its stream metrics
	sameRoute bool
}

// val reads one metric of an entry; NaN when unknown.
func (e baseEntry) val(key string, ftp float64) float64 {
	x := e.act
	sv := func(k string) float64 {
		if !e.known {
			return nan
		}
		if v, ok := e.sm.Vals[k]; ok {
			return v
		}
		return nan
	}
	switch key {
	case "distance":
		return positive(x.Distance)
	case "moving":
		return positive(float64(x.MovingTime))
	case "speed":
		return positive(x.AverageSpeed)
	case "gain":
		if x.Distance <= 0 {
			return nan
		}
		return x.TotalElevationGain
	case "avg_hr":
		if !x.HasHeartrate {
			return nan
		}
		return positive(x.AverageHeartrate)
	case "kj":
		return positive(x.Kilojoules)
	case "if":
		return intensity(sv("np"), ftp)
	case "tss":
		if !e.known {
			return nan
		}
		return tss(e.sm.MovingS, sv("np"), ftp)
	}
	return sv(key)
}

func positive(v float64) float64 {
	if v > 0 {
		return v
	}
	return nan
}

// metricRow describes one compared metric.
type metricRow struct {
	key, label string
	kind       int
	format     func(float64) string
}

func metricRows(u units, sport string) []metricRow {
	speedLabel := "Avg speed"
	if paceSport(sport) {
		speedLabel = "Avg pace"
	}
	w := func(v float64) string { return watts(v) }
	return []metricRow{
		{"distance", "Distance", kindVolume, u.dist},
		{"moving", "Moving time", kindVolume, clock},
		{"speed", speedLabel, kindPerf, func(v float64) string { return u.tempo(v, sport) }},
		{"gain", "Elevation", kindVolume, u.elev},
		{"avg_hr", "Avg HR", kindNeutral, bpm},
		{"np", "Normalized power", kindPerf, w},
		{"if", "Intensity factor", kindVolume, func(v float64) string { return num(v, 2) }},
		{"tss", "TSS", kindVolume, func(v float64) string { return num(v, 0) }},
		{"ef", "Efficiency factor", kindPerf, func(v float64) string { return num(v, 2) }},
		{"decoupling", "HR drift (decoupling)", kindLower, func(v float64) string { return num(v, 1) + " %" }},
		{"mm_5", "Best 5 s", kindPerf, w},
		{"mm_60", "Best 1 min", kindPerf, w},
		{"mm_300", "Best 5 min", kindPerf, w},
		{"mm_1200", "Best 20 min", kindPerf, w},
		{"kj", "Work", kindVolume, func(v float64) string { return thousands(v) + " kJ" }},
	}
}

// storedFrom keeps the FTP- and zone-free stream metrics of m.
func storedFrom(m rideMetrics, start time.Time) storedMetrics {
	sm := storedMetrics{V: metricsVersion, Start: start.Format("2006-01-02T15:04:05"), MovingS: m.MovingS, Vals: map[string]float64{}}
	put := func(k string, v float64) {
		if finite(v) {
			sm.Vals[k] = math.Round(v*100) / 100
		}
	}
	if m.HasWatts {
		put("np", m.NP)
		put("avg_w", m.AvgW)
		for i, d := range curveDurations {
			put("mm_"+strconv.Itoa(d), m.MeanMax[i])
		}
	}
	put("avg_hr", m.AvgHR)
	put("ef", m.EF)
	put("decoupling", m.Decoupling)
	return sm
}

// sameFamily: the baseline holds the same sport_type; with mixIndoor a
// virtual sport and its outdoor one count together.
func sameFamily(a, b string, mixIndoor bool) bool {
	if a == b {
		return true
	}
	if !mixIndoor {
		return false
	}
	return strings.TrimPrefix(a, "Virtual") == strings.TrimPrefix(b, "Virtual")
}

func sportNoun(sport string) string {
	switch {
	case strings.Contains(sport, "Ride"):
		return "rides"
	case strings.Contains(sport, "Run"):
		return "runs"
	case sport == "Swim":
		return "swims"
	}
	return "activities"
}

// comparison is everything the analysis says about the window before the
// activity.
type comparison struct {
	weeks          int
	mixIndoor      bool
	from           time.Time // Monday 00:00 (local) of the window's first week
	sports         []string
	entries        []baseEntry // newest first
	known, missing int
	stoppedBy      string
	same           []baseEntry
	gaps           [][]float64 // per same-route ride, at gapStepM steps
	table          *weekTable
}

// storeMetrics merges add into the user's metric file, unless the user
// disconnected meanwhile (checked under the lock disconnect holds).
func (t *tools) storeMetrics(user string, athleteID int64, add map[string]storedMetrics) {
	if len(add) == 0 {
		return
	}
	sl := t.svc.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	// Another athlete than the one the metrics were read for (a reconnect
	// to a different Strava account meanwhile) must not inherit them.
	if c, ok := t.svc.conn(user); !ok || (c.AthleteID != 0 && c.AthleteID != athleteID) {
		return
	}
	m, _ := t.svc.store.loadMetrics(user)
	for k, v := range add {
		m.Activities[k] = v
	}
	if err := t.svc.store.saveMetrics(user, m); err != nil {
		// Only a cost: the streams are fetched again next time.
		slog.Error("strava: saving metrics failed", "err", err)
	}
}

// compare builds the window: one list call for the summaries, streams for
// the activities whose stream metrics are not stored yet, newest first,
// within the rate and time budget.
func (t *tools) compare(ctx context.Context, user string, a athlete, d detailedActivity, weeks int, mixIndoor bool, this storedMetrics) (*comparison, error) {
	local, utc := d.localStart(), d.utcStart()
	offset := local.Sub(utc)
	c := &comparison{weeks: weeks, mixIndoor: mixIndoor, from: weekStart(local).AddDate(0, 0, -7*weeks)}
	list, _, err := t.svc.listActivities(ctx, user, c.from.Add(-offset).Unix()-1, utc.Unix(), maxListPages)
	if err != nil {
		return nil, err
	}
	stored := t.loadMetrics(user)
	all := []summaryActivity{}
	for _, x := range list {
		ls := x.localStart()
		if x.ID == d.ID || ls.Before(c.from) || !ls.Before(local) {
			continue
		}
		all = append(all, x)
		if sameFamily(x.SportType, d.SportType, mixIndoor) {
			sm, ok := stored[strconv.FormatInt(x.ID, 10)]
			c.entries = append(c.entries, baseEntry{act: x, sm: sm, known: ok})
		}
	}
	slices.SortStableFunc(c.entries, func(p, q baseEntry) int { return q.act.localStart().Compare(p.act.localStart()) })

	// Cold start: streams newest first while the budget lasts.
	added := map[string]storedMetrics{strconv.FormatInt(d.ID, 10): this}
	fetched := 0
	for i := range c.entries {
		e := &c.entries[i]
		if e.known || e.act.Manual {
			continue
		}
		if reason := t.budgetLeft(ctx, fetched); reason != "" {
			c.stoppedBy = reason
			break
		}
		sctx, cancel := context.WithTimeout(ctx, streamTimeout)
		s, err := t.svc.streams(sctx, user, e.act.ID)
		cancel()
		fetched++
		var rl *rateLimitedError
		if errors.As(err, &rl) {
			c.stoppedBy = "the Strava rate budget"
			break
		}
		if err != nil {
			continue
		}
		m := computeMetrics(s, 0, s.n, nil, nil)
		e.sm, e.known = storedFrom(m, e.act.localStart()), true
		added[strconv.FormatInt(e.act.ID, 10)] = e.sm
	}
	t.storeMetrics(user, a.ID, added)
	for _, e := range c.entries {
		switch {
		case e.known:
			c.known++
		case !e.act.Manual:
			c.missing++
		}
	}

	// Like for like: the same start, end and distance.
	if d.StartLatLng.ok() && d.EndLatLng.ok() && d.Distance > 0 {
		for i := range c.entries {
			e := &c.entries[i]
			x := e.act
			if !x.StartLatLng.ok() || !x.EndLatLng.ok() {
				continue
			}
			if haversine(x.StartLatLng, d.StartLatLng) <= sameRouteM && haversine(x.EndLatLng, d.EndLatLng) <= sameRouteM &&
				math.Abs(x.Distance-d.Distance)/d.Distance <= sameRouteDist {
				e.sameRoute = true
				if len(c.same) < maxSameRoute {
					c.same = append(c.same, *e)
				}
			}
		}
	}

	// Weeks: every sport of the window, so a load chart can stack them.
	c.table = newWeekTable(c.from, local, nil)
	for _, x := range all {
		sm, ok := stored[strconv.FormatInt(x.ID, 10)]
		if s, ok2 := added[strconv.FormatInt(x.ID, 10)]; ok2 {
			sm, ok = s, true
		}
		ef := nan
		if ok {
			if v, has := sm.Vals["ef"]; has {
				ef = v
			}
		}
		c.table.add(x, activityTSS(x, sm, ok, a.FTP), ef)
	}
	thisEF := nan
	if v, ok := this.Vals["ef"]; ok {
		thisEF = v
	}
	c.table.add(d.summaryActivity, activityTSS(d.summaryActivity, this, true, a.FTP), thisEF)
	for _, e := range c.entries {
		if !slices.Contains(c.sports, e.act.SportType) {
			c.sports = append(c.sports, e.act.SportType)
		}
	}
	if !slices.Contains(c.sports, d.SportType) {
		c.sports = append([]string{d.SportType}, c.sports...)
	}
	return c, nil
}

// budgetLeft says why the baseline must stop reading streams; "" to go on.
func (t *tools) budgetLeft(ctx context.Context, fetched int) string {
	if fetched >= maxColdFetches {
		return "the per-call limit"
	}
	if t.svc.rate.fraction15(t.svc.now()) >= coldBudget {
		return "the Strava rate budget"
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < minTimeLeft {
		return "the time limit"
	}
	return ""
}

// rank is this value's place among vals (this included): 1 is the highest,
// or the lowest for kindLower. Ties share the better place; tied says one
// did.
func rank(this float64, vals []float64, kind int) (r, n int, tied bool) {
	r, n = 1, 1
	for _, v := range vals {
		if math.IsNaN(v) {
			continue
		}
		n++
		switch {
		case v == this:
			tied = true
		case (kind == kindLower && v < this) || (kind != kindLower && v > this):
			r++
		}
	}
	return r, n, tied
}

// flag marks the best or worst of the window, never a shared place.
func flag(kind, r, n int, tied bool) string {
	if n < minFlagN || tied {
		return ""
	}
	switch kind {
	case kindVolume:
		if r == 1 {
			return "highest in window"
		}
		if r == n {
			return "lowest in window"
		}
	case kindPerf, kindLower:
		if r == 1 {
			return "best in window"
		}
		if r == n {
			return "worst in window"
		}
	}
	return ""
}

func rankText(kind, r, n int) string {
	switch kind {
	case kindPerf, kindLower:
		return fmt.Sprintf("%s best of %d", ordinal(r), n)
	default:
		return fmt.Sprintf("%s highest of %d", ordinal(r), n)
	}
}

type analysisArgs struct {
	ID        json.RawMessage `json:"id"`
	Weeks     int             `json:"weeks"`
	MixIndoor bool            `json:"mix_indoor"`
	FromKm    *float64        `json:"from_km"`
	ToKm      *float64        `json:"to_km"`
}

func (t *tools) activityAnalysis(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args analysisArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("activity_analysis: " + err.Error())
	}
	id, ok := parseID(args.ID)
	if !ok {
		return mcpserver.ErrorResult("activity_analysis needs an activity id")
	}
	if args.Weeks == 0 {
		args.Weeks = defaultWeeks
	}
	if args.Weeks < 1 || args.Weeks > maxWeeks {
		return mcpserver.ErrorResult(fmt.Sprintf("weeks must be 1 to %d", maxWeeks))
	}
	section := args.FromKm != nil || args.ToKm != nil
	fromM, toM := 0.0, math.Inf(1)
	if args.FromKm != nil {
		fromM = *args.FromKm * 1000
	}
	if args.ToKm != nil {
		toM = *args.ToKm * 1000
	}
	if fromM < 0 || toM <= fromM {
		return mcpserver.ErrorResult("from_km must be below to_km")
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	d, res, ok := t.ownActivity(ctx, user, a, id)
	if !ok {
		return res
	}
	z, zerr := t.svc.zones(ctx, user)
	if zerr != nil {
		z = athleteZones{}
	}
	s, err := t.svc.streams(ctx, user, id)
	if err != nil {
		return failure(err)
	}
	u := unitsOf(a)
	pE, hE := powerEdges(z, a.FTP), hrEdges(z)
	whole := computeMetrics(s, 0, s.n, pE, hE)
	ia, ib := 0, s.n
	if section {
		if s.dist == nil {
			return mcpserver.ErrorResult("This activity has no distance stream, so it has no sections by km.")
		}
		ia, ib = s.sub(fromM, toM)
		if ib-ia < 2 {
			return mcpserver.ErrorResult(fmt.Sprintf("The activity is %s long: no data between km %.1f and %.1f.", u.dist(d.Distance), fromM/1000, math.Min(toM, 1e9)/1000))
		}
	}
	m := whole
	if section {
		m = computeMetrics(s, ia, ib, pE, hE)
	}
	this := storedFrom(whole, d.localStart())

	// Strava's own HR zone times need a subscription; ours come from the
	// streams with the athlete's zones either way.
	zoneNote := ""
	if !section && s.n > 0 && hE != nil && s.hr != nil {
		azs, err := t.svc.activityZones(ctx, user, id)
		switch {
		case summitRefused(err):
			zoneNote = "computed from the streams: Strava's own activity zones " + msgSubscription
		case err == nil:
			for _, az := range azs {
				if az.Type == "heartrate" && len(az.Buckets) == 5 {
					for i, bk := range az.Buckets {
						m.HRZoneS[i] = int(bk.Time)
					}
					zoneNote = "HR zone times from Strava"
				}
			}
		}
	}

	cmp, cmpErr := t.compare(ctx, user, a, d, args.Weeks, args.MixIndoor, this)

	var b strings.Builder
	sources := []mcpserver.Source{activitySource(d.summaryActivity)}
	head := fmt.Sprintf("[1] Activity %d · %s · %s · %s", d.ID, d.Name, d.SportType, day(d.localStart()))
	if d.DeviceName != "" {
		head += " · " + d.DeviceName
	}
	if d.Gear != nil && d.Gear.Name != "" {
		head += " · " + d.Gear.Name
	}
	b.WriteString(head + "\n")
	if section {
		fmt.Fprintf(&b, "section km %.1f–%.1f (%s of the whole %s): every number below is the section's; the comparison is the whole activity's\n",
			s.dist[ia]/1000, s.dist[ib-1]/1000, u.dist(m.DistanceM), u.dist(d.Distance))
		fmt.Fprintf(&b, "section: %s · %s · elevation gain %s · avg %s\n", u.dist(m.DistanceM), clock(float64(m.Samples)), u.elev(m.GainM), u.tempo(m.AvgSpeed, d.SportType))
	} else {
		fmt.Fprintf(&b, "distance %s · moving %s · elapsed %s · elevation %s · avg %s\n", u.dist(d.Distance), clock(float64(d.MovingTime)),
			clock(float64(d.ElapsedTime)), u.elev(d.TotalElevationGain), u.tempo(d.AverageSpeed, d.SportType))
	}
	if s.n == 0 {
		b.WriteString("no streams recorded (a manual entry, or a device that sent none): summary metrics only\n")
	}
	if m.HasWatts {
		line := fmt.Sprintf("power: avg %s · NP %s · VI %s", watts(m.AvgW), watts(m.NP), num(m.NP/m.AvgW, 2))
		if a.FTP > 0 {
			line += fmt.Sprintf(" · IF %s · TSS %s", num(intensity(m.NP, a.FTP), 2), num(tss(m.MovingS, m.NP, a.FTP), 0))
		}
		line += fmt.Sprintf(" · %s kJ · max %s", thousands(m.KJ), watts(m.MaxW))
		switch {
		case a.FTP > 0 && a.Weight > 0:
			line += fmt.Sprintf(" (FTP %.0f W, %.0f kg)", a.FTP, a.Weight)
		case a.FTP > 0:
			line += fmt.Sprintf(" (FTP %.0f W)", a.FTP)
		default:
			line += " (no FTP set on Strava: no IF or TSS)"
		}
		b.WriteString(line + "\n")
	}
	var parts []string
	if m.HasHR {
		parts = append(parts, fmt.Sprintf("heart rate: avg %s · max %s", bpm(m.AvgHR), bpm(m.MaxHR)))
		if finite(m.EF) {
			parts = append(parts, "EF "+num(m.EF, 2))
		}
		if finite(m.Decoupling) {
			parts = append(parts, "decoupling "+num(m.Decoupling, 1)+" %")
		}
	}
	if finite(m.AvgCad) {
		parts = append(parts, fmt.Sprintf("cadence avg %.0f · max %.0f", m.AvgCad, m.MaxCad))
	}
	if finite(m.AvgTemp) {
		parts = append(parts, fmt.Sprintf("temp avg %.0f °C (%.0f–%.0f)", m.AvgTemp, m.MinTemp, m.MaxTemp))
	}
	if finite(m.MaxGrade) {
		parts = append(parts, fmt.Sprintf("grade %.1f to %.1f %%", m.MinGrade, m.MaxGrade))
	}
	if m.StoppedS > 0 {
		parts = append(parts, "stopped "+clock(float64(m.StoppedS)))
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, " · ") + "\n")
	}
	if m.HasPowerZones && m.MovingS > 0 {
		b.WriteString("time in power zones Z1..Z5+ (30 s power): " + zoneText(m.PowerZoneS) + "\n")
	}
	if m.HasHRZones && m.MovingS > 0 {
		line := "time in HR zones Z1..Z5: " + zoneText(m.HRZoneS)
		if zoneNote != "" {
			line += " (" + zoneNote + ")"
		}
		b.WriteString(line + "\n")
	}
	if m.HasWatts {
		var mm []string
		for _, dur := range shownDurations {
			if v := m.MeanMax[slices.Index(curveDurations, dur)]; finite(v) {
				mm = append(mm, durationName(dur)+" "+watts(v))
			}
		}
		b.WriteString("mean-max power: " + strings.Join(mm, " · ") + "\n")
	}

	cl := climbs(s, ia, ib, a.Weight)
	if len(cl) > 0 {
		b.WriteString("\nclimbs (≥ 500 m at ≥ 3 %):\n")
		for i, c := range cl {
			fmt.Fprintf(&b, "  %d. %s %s–%s · %s · %s · %.1f %% · %s · VAM %s m/h", i+1, u.splitName(), u.distNum(c.FromM), u.distNum(c.ToM),
				u.dist(c.ToM-c.FromM), u.elev(c.GainM), c.GradePct, clock(float64(c.Secs)), thousands(c.VAM))
			if finite(c.AvgW) {
				b.WriteString(" · " + watts(c.AvgW))
				if finite(c.WKg) {
					fmt.Fprintf(&b, " · %.2f W/kg", c.WKg)
				}
			}
			if finite(c.AvgHR) {
				fmt.Fprintf(&b, " · HR %.0f", c.AvgHR)
			}
			b.WriteByte('\n')
		}
	}

	writeIntervals(&b, d, s, ia, ib, a.FTP, u, section)

	if cmpErr != nil {
		b.WriteString("\ncomparison with earlier activities unavailable: " + failure(cmpErr).Text + "\n")
	} else {
		sources = t.writeComparison(ctx, &b, user, cmp, d, whole, a, u, sources, s)
	}

	rows, width := kmRows(s, ia, ib, u.splitM())
	if len(rows) > 0 {
		unit := u.splitName()
		if width != u.splitM() {
			unit = fmt.Sprintf("%.0f %s", width/u.splitM(), u.splitName())
		}
		speedHead := "speed"
		if paceSport(d.SportType) {
			speedHead = "pace"
		}
		fmt.Fprintf(&b, "\nper %s (%s | time | %s | W | bpm | rpm | alt | grade):\n", unit, u.splitName(), speedHead)
		for i, r := range rows {
			line := fmt.Sprintf("%s | %s | %s | %s | %s | %s | %s | %s\n", u.distNum(r.ToM), clock(r.Secs), tempoShort(r.SpeedMS, d.SportType, u),
				num(r.W, 0), num(r.HR, 0), num(r.Cad, 0), elevShort(r.AltM, u), num(r.GradePct, 1))
			if b.Len()+len(line) > textBudget {
				fmt.Fprintf(&b, "… %d more rows in the streams file\n", len(rows)-i)
				break
			}
			b.WriteString(line)
		}
	}

	files := analysisFiles(d, s, whole, cl, cmp, a, pE, hE)
	b.WriteString(fileNames(files))
	return mcpserver.Result{Text: b.String(), Sources: sources, Files: files}
}

func tempoShort(ms float64, sport string, u units) string {
	if !finite(ms) {
		return "-"
	}
	v := u.tempo(ms, sport)
	if i := strings.IndexByte(v, ' '); i > 0 {
		return v[:i]
	}
	return v
}

func elevShort(m float64, u units) string {
	if !finite(m) {
		return "-"
	}
	if u.imperial {
		return fmt.Sprintf("%.0f", m*feetPerMeter)
	}
	return fmt.Sprintf("%.0f", m)
}

func zoneText(z [5]int) string {
	total := 0
	for _, s := range z {
		total += s
	}
	parts := make([]string, 5)
	for i, s := range z {
		pct := 0.0
		if total > 0 {
			pct = float64(s) / float64(total) * 100
		}
		parts[i] = fmt.Sprintf("%.0f %% (%s)", pct, clock(float64(s)))
	}
	return strings.Join(parts, " · ")
}

// writeIntervals lists the laps the device wrote, or else the hard efforts.
func writeIntervals(b *strings.Builder, d detailedActivity, s series, ia, ib int, ftp float64, u units, section bool) {
	if len(d.Laps) > 1 && !section {
		fmt.Fprintf(b, "\nlaps (%d):\n", len(d.Laps))
		for i, l := range d.Laps {
			if i == maxLaps {
				fmt.Fprintf(b, "  … %d more laps\n", len(d.Laps)-maxLaps)
				break
			}
			fmt.Fprintf(b, "  %d. %s · %s · %s", i+1, u.dist(l.Distance), clock(float64(l.MovingTime)), u.tempo(l.AverageSpeed, d.SportType))
			if l.AverageWatts > 0 {
				fmt.Fprintf(b, " · %.0f W", l.AverageWatts)
				if ftp > 0 {
					fmt.Fprintf(b, " (%.0f %% FTP)", l.AverageWatts/ftp*100)
				}
			}
			if l.AverageHeartrate > 0 {
				fmt.Fprintf(b, " · HR %.0f/%.0f", l.AverageHeartrate, l.MaxHeartrate)
			}
			b.WriteByte('\n')
		}
		return
	}
	ef := efforts(s, ia, ib, ftp)
	if len(ef) == 0 {
		return
	}
	fmt.Fprintf(b, "\nhard efforts (≥ 30 s above 105 %% FTP, %d):\n", len(ef))
	for i, e := range ef {
		if i == maxIntervals {
			fmt.Fprintf(b, "  … %d more\n", len(ef)-maxIntervals)
			break
		}
		at := ""
		if s.dist != nil {
			at = fmt.Sprintf(" at %s %s", u.splitName(), u.distNum(s.dist[e.A]))
		}
		fmt.Fprintf(b, "  %d.%s · %s · %s (%.0f %% FTP)", i+1, at, clock(float64(e.B-e.A)), watts(e.AvgW), e.AvgW/ftp*100)
		if finite(e.AvgHR) {
			fmt.Fprintf(b, " · HR avg %.0f, %.0f at the end", e.AvgHR, e.EndHR)
		}
		b.WriteByte('\n')
	}
}

// writeComparison writes the window block and returns the sources with the
// same-route rides appended.
func (t *tools) writeComparison(ctx context.Context, b *strings.Builder, user string, c *comparison, d detailedActivity, whole rideMetrics,
	a athlete, u units, sources []mcpserver.Source, s series) []mcpserver.Source {
	noun := sportNoun(d.SportType)
	thisEntry := baseEntry{act: d.summaryActivity, sm: storedFrom(whole, d.localStart()), known: true}
	end := d.localStart().AddDate(0, 0, -1)
	fmt.Fprintf(b, "\ncompared with %d %s (%s), %s – %s, the %d weeks before this one and this week so far",
		len(c.entries), noun, strings.Join(c.sports, " + "), c.from.Format("2006-01-02"), end.Format("2006-01-02"), c.weeks)
	if !c.mixIndoor {
		b.WriteString("; indoor and outdoor kept apart (mix_indoor joins them)")
	}
	b.WriteString(":\n")
	if len(c.entries) == 0 {
		b.WriteString("  no earlier " + noun + " in the window\n")
	}
	if c.missing > 0 {
		reason := c.stoppedBy
		if reason == "" {
			reason = "Strava errors"
		}
		fmt.Fprintf(b, "  partial: stream metrics (NP, IF, TSS, EF, decoupling, power bests) from %d of %d %s, stopped by %s; the next call reads the rest\n",
			c.known, c.known+c.missing, noun, reason)
	}
	if len(c.entries) > 0 {
		b.WriteString("  metric | this | median | Δ | rank\n")
		for _, r := range metricRows(u, d.SportType) {
			v := thisEntry.val(r.key, a.FTP)
			if !finite(v) {
				continue
			}
			vals := make([]float64, 0, len(c.entries))
			for _, e := range c.entries {
				vals = append(vals, e.val(r.key, a.FTP))
			}
			med := median(vals)
			if !finite(med) {
				continue
			}
			rk, n, tied := rank(v, vals, r.kind)
			delta := "-"
			if med != 0 {
				delta = fmt.Sprintf("%+.0f %%", (v-med)/math.Abs(med)*100)
			}
			line := fmt.Sprintf("  %s | %s | %s | %s | %s", r.label, r.format(v), r.format(med), delta, rankText(r.kind, rk, n))
			if f := flag(r.kind, rk, n, tied); f != "" {
				line += " · " + f
			}
			b.WriteString(line + "\n")
		}
		if whole.HasWatts {
			var bests []string
			for _, dur := range curveDurations {
				k := "mm_" + strconv.Itoa(dur)
				v := thisEntry.val(k, a.FTP)
				best := nan
				for _, e := range c.entries {
					if x := e.val(k, a.FTP); finite(x) && (!finite(best) || x > best) {
						best = x
					}
				}
				if finite(v) && finite(best) && v > best {
					bests = append(bests, durationName(dur))
				}
			}
			if len(bests) > 0 {
				fmt.Fprintf(b, "  new %d-week power bests: %s\n", c.weeks, strings.Join(bests, ", "))
			}
		}
	}

	if len(c.same) > 0 {
		b.WriteString("\nsame route (start and end within 200 m, distance ± 5 %), most recent first:\n")
		thisAt := []float64(nil)
		if s.dist != nil {
			thisAt = elapsedAt(s, gapStepM, int(d.Distance/gapStepM)+1)
		}
		for _, e := range c.same {
			x := e.act
			n := len(sources)
			sources = append(sources, activitySource(x))
			line := fmt.Sprintf("  %s %s · %s · moving %s · elapsed %s · avg %s", marker(n), x.localStart().Format("2006-01-02"), x.Name,
				clock(float64(x.MovingTime)), clock(float64(x.ElapsedTime)), u.tempo(x.AverageSpeed, x.SportType))
			for _, k := range []struct{ key, name string }{{"np", "NP"}, {"ef", "EF"}, {"decoupling", "decoupling"}, {"mm_1200", "best 20 min"}} {
				if v := e.val(k.key, a.FTP); finite(v) {
					switch k.key {
					case "np", "mm_1200":
						line += " · " + k.name + " " + watts(v)
					case "ef":
						line += " · EF " + num(v, 2)
					default:
						line += " · decoupling " + num(v, 1) + " %"
					}
				}
			}
			var gaps []float64
			if thisAt != nil && t.svc.rate.fraction15(t.svc.now()) < refuseFraction {
				sctx, cancel := context.WithTimeout(ctx, streamTimeout)
				os2, err := t.svc.streams(sctx, user, x.ID)
				cancel()
				if err == nil && os2.dist != nil {
					other := elapsedAt(os2, gapStepM, len(thisAt))
					gaps = make([]float64, len(thisAt))
					for i := range gaps {
						gaps[i] = thisAt[i] - other[i]
					}
					if g := lastFinite(gaps); finite(g) {
						ahead := "behind"
						if g < 0 {
							ahead = "ahead"
						}
						line += fmt.Sprintf(" · this one %s %s at the finish", clock(math.Abs(g)), ahead)
					}
				}
			}
			c.gaps = append(c.gaps, gaps)
			b.WriteString(line + "\n")
		}
	}

	// Load: this ISO week so far against the weekly average of the weeks
	// before it, by TSS, or by moving time without power.
	byTSS := whole.HasWatts && a.FTP > 0
	weeks := c.table.weeks()
	sums := make([]float64, len(weeks))
	counts := make([]int, len(weeks))
	for i, row := range weeks {
		for _, g := range row {
			if !slices.Contains(c.sports, g.sport) {
				continue
			}
			counts[i] += g.count
			if byTSS {
				sums[i] += g.tss
			} else {
				sums[i] += g.movingS
			}
		}
	}
	if n := len(sums); n >= 2 {
		cur := sums[n-1]
		avg := 0.0
		heaviest := true
		for _, v := range sums[:n-1] {
			avg += v
			heaviest = heaviest && cur > v
		}
		avg /= float64(n - 1)
		what, fmtv := "TSS", func(v float64) string { return num(v, 0) }
		if !byTSS {
			what, fmtv = "moving time", clock
		}
		fmt.Fprintf(b, "\nload: this week (Mon–Sun, up to this activity) %s %s (%s) vs weekly avg %s of the %d weeks before", what, fmtv(cur),
			plural(counts[n-1], strings.TrimSuffix(noun, "s"), noun), fmtv(avg), n-1)
		if heaviest && cur > 0 {
			b.WriteString(" · heaviest week in the window")
		}
		b.WriteString("\nweeks:")
		for i, row := range weeks {
			efs := []float64{}
			for _, g := range row {
				if slices.Contains(c.sports, g.sport) {
					efs = append(efs, g.efs...)
				}
			}
			fmt.Fprintf(b, " %s %s %s (%d)", isoWeek(row[0].start)[5:], what, fmtv(sums[i]), counts[i])
			if med := median(efs); finite(med) {
				fmt.Fprintf(b, " EF %s", num(med, 2))
			}
			if i < len(weeks)-1 {
				b.WriteString(" ·")
			}
		}
		b.WriteString("\n")
		if byTSS {
			b.WriteString("  (TSS from stream NP where stored, else from Strava's weighted power)\n")
		}
	}
	return sources
}

func lastFinite(v []float64) float64 {
	for i := len(v) - 1; i >= 0; i-- {
		if finite(v[i]) {
			return v[i]
		}
	}
	return nan
}

// analysisFiles builds the CSV files, the streams first and as fine as the
// 3 MiB of one result allow.
func analysisFiles(d detailedActivity, s series, whole rideMetrics, cl []climb, c *comparison, a athlete, pE, hE []float64) []mcpserver.File {
	var files []mcpserver.File
	if len(cl) > 0 {
		t := newCSV(hdrClimbs)
		for i, x := range cl {
			t.row(intCell(i+1), cell(x.FromM/1000, 2), cell(x.ToM/1000, 2), cell((x.ToM-x.FromM)/1000, 2), cell(x.GainM, 0),
				cell(x.GradePct, 1), intCell(x.Secs), cell(x.VAM, 0), cell(x.AvgW, 0), cell(x.WKg, 2), cell(x.AvgHR, 0))
		}
		files = append(files, t.file(fmt.Sprintf("strava_%d_climbs.csv", d.ID)))
	}
	if c != nil {
		if whole.HasWatts {
			t := newCSV(hdrPowerCurve)
			for i, dur := range curveDurations {
				k := "mm_" + strconv.Itoa(dur)
				vals := []float64{}
				for _, e := range c.entries {
					vals = append(vals, e.val(k, a.FTP))
				}
				t.row(intCell(dur), cell(whole.MeanMax[i], 0), cell(maxOf(knownOnly(vals)), 0), cell(median(vals), 0))
			}
			files = append(files, t.file(fmt.Sprintf("strava_%d_power_curve.csv", d.ID)))
		}
		t := newCSV(hdrBaseline)
		entries := append([]baseEntry{{act: d.summaryActivity, sm: storedFrom(whole, d.localStart()), known: true}}, c.entries...)
		for i, e := range entries {
			x := e.act
			t.row(strconv.FormatInt(x.ID, 10), x.localStart().Format("2006-01-02"), x.Name, x.SportType, boolCell(i == 0), boolCell(e.sameRoute),
				cell(x.Distance/1000, 2), intCell(x.MovingTime), cell(x.AverageSpeed*3.6, 1), cell(x.TotalElevationGain, 0),
				cell(e.val("np", a.FTP), 0), cell(e.val("tss", a.FTP), 0), cell(e.val("ef", a.FTP), 2), cell(e.val("decoupling", a.FTP), 1),
				cell(e.val("mm_1200", a.FTP), 0), cell(e.val("avg_hr", a.FTP), 0))
		}
		files = append(files, t.file(fmt.Sprintf("strava_%d_baseline.csv", d.ID)))
		if f, ok := sameRouteCSV(d.ID, c); ok {
			files = append(files, f)
		}
		files = append(files, c.table.file())
	}
	if s.n > 0 {
		if f, ok := streamsCSV(d.ID, s, pE, hE, maxFilesBytes-filesSize(files)); ok {
			files = append([]mcpserver.File{f}, files...)
		}
	}
	return files
}

func knownOnly(v []float64) []float64 {
	out := []float64{}
	for _, x := range v {
		if finite(x) {
			out = append(out, x)
		}
	}
	return out
}

// sameRouteCSV: one gap column per earlier same-route ride, this one's
// elapsed time minus that one's every 0.2 km; negative = ahead.
func sameRouteCSV(id int64, c *comparison) (mcpserver.File, bool) {
	header := []string{hdrSameRoutePrefix}
	var cols [][]float64
	seen := map[string]int{}
	rows := 0
	for i, e := range c.same {
		if i >= len(c.gaps) || c.gaps[i] == nil {
			continue
		}
		name := "gap_s_" + e.act.localStart().Format("2006-01-02")
		seen[name]++
		if seen[name] > 1 {
			name += "_" + strconv.Itoa(seen[name])
		}
		header = append(header, name)
		cols = append(cols, c.gaps[i])
		rows = max(rows, len(c.gaps[i]))
	}
	if len(cols) == 0 {
		return mcpserver.File{}, false
	}
	t := newCSV(strings.Join(header, ","))
	for k := range rows {
		row := []string{cell(float64(k)*gapStepM/1000, 1)}
		for _, col := range cols {
			v := nan
			if k < len(col) {
				v = col[k]
			}
			row = append(row, cell(v, 0))
		}
		t.row(row...)
	}
	return t.file(fmt.Sprintf("strava_%d_same_route.csv", id)), true
}

func (t *tools) compareActivities(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		IDs []json.RawMessage `json:"ids"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("compare_activities: " + err.Error())
	}
	var ids []int64
	for _, r := range args.IDs {
		id, ok := parseID(r)
		if !ok {
			return mcpserver.ErrorResult("compare_activities: every id must be an activity id")
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 || len(ids) > maxCompare {
		return mcpserver.ErrorResult(fmt.Sprintf("compare_activities needs 2 to %d different activity ids", maxCompare))
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	u := unitsOf(a)
	stored := t.loadMetrics(user)
	added := map[string]storedMetrics{}
	entries := make([]baseEntry, 0, len(ids))
	for _, id := range ids {
		d, res, ok := t.ownActivity(ctx, user, a, id)
		if !ok {
			return res
		}
		key := strconv.FormatInt(id, 10)
		sm, known := stored[key]
		if !known && !d.Manual {
			s, err := t.svc.streams(ctx, user, id)
			if err != nil {
				return failure(err)
			}
			sm, known = storedFrom(computeMetrics(s, 0, s.n, nil, nil), d.localStart()), true
			added[key] = sm
		}
		entries = append(entries, baseEntry{act: d.summaryActivity, sm: sm, known: known})
	}
	t.storeMetrics(user, a.ID, added)

	var b strings.Builder
	sources := make([]mcpserver.Source, 0, len(entries))
	b.WriteString("Strava activities side by side:\n")
	for i, e := range entries {
		fmt.Fprintf(&b, "%s %s · %s · %s · id %d\n", marker(i), day(e.act.localStart()), e.act.Name, e.act.SportType, e.act.ID)
		sources = append(sources, activitySource(e.act))
	}
	head := []string{"metric"}
	for i := range entries {
		head = append(head, marker(i))
	}
	b.WriteString(strings.Join(head, " | ") + "\n")
	for _, r := range metricRows(u, entries[0].act.SportType) {
		cells := []string{r.label}
		known := false
		for _, e := range entries {
			v := e.val(r.key, a.FTP)
			if finite(v) {
				known = true
				cells = append(cells, r.format(v))
			} else {
				cells = append(cells, "-")
			}
		}
		if known {
			b.WriteString(strings.Join(cells, " | ") + "\n")
		}
	}
	sameStart := true
	for _, e := range entries[1:] {
		x, y := e.act, entries[0].act
		sameStart = sameStart && x.StartLatLng.ok() && y.StartLatLng.ok() && haversine(x.StartLatLng, y.StartLatLng) <= sameRouteM &&
			x.EndLatLng.ok() && y.EndLatLng.ok() && haversine(x.EndLatLng, y.EndLatLng) <= sameRouteM && y.Distance > 0 &&
			math.Abs(x.Distance-y.Distance)/y.Distance <= sameRouteDist
	}
	if sameStart {
		b.WriteString("all on the same route (start and end within 200 m, distance ± 5 %)\n")
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}
