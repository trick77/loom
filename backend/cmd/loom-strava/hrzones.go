package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/mcpserver"
)

// hrFamily is the sports that share one threshold heart rate. Running sits
// 5–10 bpm above cycling, so the two are estimated apart; an e-bike, a walk
// or a hike never reaches threshold and is left out.
type hrFamily struct {
	key    string
	sports []string
	// pct are the lower bounds of zones 2..5 in percent of the LTHR:
	// Friel's zones, his 5a–5c folded into Z5.
	pct [4]float64
}

var hrFamilies = []hrFamily{
	{"cycling", []string{"Ride", "GravelRide", "MountainBikeRide", "VirtualRide"}, [4]float64{81, 90, 94, 100}},
	{"running", []string{"Run", "TrailRun", "VirtualRun"}, [4]float64{85, 90, 95, 100}},
}

const (
	// hrWindowDays: a year of activities, so a season's hard efforts count
	// and an old fitness level does not.
	hrWindowDays = 365
	// maxConfirmBPM: a max heart rate counts only when a second activity
	// comes this close; a strap spike or a cadence lock is one activity's.
	maxConfirmBPM = 5
	// lthrShare of the best 20 min is the LTHR (Friel's 20-minute test).
	lthrShare = 0.95
	// lowEffortShare: a best 20 min under this share of the max heart rate
	// was no threshold effort.
	lowEffortShare = 0.85
	// thresholdFTP: 20 min at this share of FTP or more is a threshold effort.
	thresholdFTP = 0.95
	// ftpRecentDays: only a best 20 min this recent is held against today's
	// FTP; an older one was ridden at another fitness.
	ftpRecentDays = 90
	// hrZonesMaxAge: stored zones older than this are recalculated when
	// read, so efforts that left the window stop counting.
	hrZonesMaxAge = 7 * 24 * time.Hour
)

func familyOf(sport string) (hrFamily, bool) {
	for _, f := range hrFamilies {
		if slices.Contains(f.sports, sport) {
			return f, true
		}
	}
	return hrFamily{}, false
}

// zones are the family's five zones in bpm for lthr.
func (f hrFamily) zones(lthr float64) []zoneRange {
	var e [4]float64
	for i, p := range f.pct {
		e[i] = math.Round(p * lthr / 100)
	}
	return []zoneRange{{0, e[0] - 1}, {e[0], e[1] - 1}, {e[1], e[2] - 1}, {e[2], e[3] - 1}, {e[3], -1}}
}

// hrRef is an activity an estimate came from: enough to cite it.
type hrRef struct {
	ID    int64  `json:"id"`
	Start string `json:"start"` // start_date_local
	Name  string `json:"name,omitempty"`
	Sport string `json:"sport,omitempty"`
}

func (r hrRef) date() string { d, _, _ := strings.Cut(r.Start, "T"); return d }

func (r hrRef) source() mcpserver.Source {
	return mcpserver.Source{URL: activityURL(r.ID), Title: r.Name, Label: label, Snippet: r.Sport + " · " + r.date()}
}

// storedHRZones is one family's zones as last calculated, kept in the
// user's metric file so every tool reads them without Strava. Zero is
// unknown (JSON has no NaN).
type storedHRZones struct {
	CalculatedAt time.Time `json:"calculated_at"`
	From         string    `json:"from"` // first day of the window
	// WithHR is the family's activities with heart rate in the window as
	// hr_zones last listed them; Used how many the estimate rests on.
	WithHR    int         `json:"with_hr"`
	Used      int         `json:"used"`
	MaxHR     float64     `json:"max_hr,omitempty"`
	MaxAt     *hrRef      `json:"max_at,omitempty"`
	ConfirmHR float64     `json:"confirm_hr,omitempty"`
	ConfirmAt *hrRef      `json:"confirm_at,omitempty"`
	Best20    float64     `json:"best_20m,omitempty"`
	Best20W   float64     `json:"best_20m_w,omitempty"`
	Best20At  *hrRef      `json:"best_20m_at,omitempty"`
	LTHR      float64     `json:"lthr,omitempty"`
	Zones     []zoneRange `json:"zones,omitempty"`
}

// edges are the lower bounds of zones 2..5, as hrEdges gives Strava's; nil
// without zones.
func (z storedHRZones) edges() []float64 {
	if len(z.Zones) != 5 {
		return nil
	}
	return []float64{z.Zones[1].Min, z.Zones[2].Min, z.Zones[3].Min, z.Zones[4].Min}
}

// hrPoint is one activity's stored values.
type hrPoint struct {
	ref  hrRef
	vals map[string]float64
}

// hrEstimate is NaN where the activities do not tell.
type hrEstimate struct {
	MaxHR, ConfirmHR float64
	MaxAt, ConfirmAt hrRef
	Best20, Best20W  float64 // the best 20 min mean HR and the mean power over those 20 min
	Best20At         hrRef
	LTHR             float64
}

func estimateHR(pts []hrPoint) hrEstimate {
	e := hrEstimate{MaxHR: nan, ConfirmHR: nan, Best20: nan, Best20W: nan, LTHR: nan}
	var top []hrPoint
	for _, p := range pts {
		if _, ok := p.vals["hr_max10"]; ok {
			top = append(top, p)
		}
		if v, ok := p.vals["hr_20m"]; ok && (math.IsNaN(e.Best20) || v > e.Best20) {
			e.Best20, e.Best20At, e.Best20W = v, p.ref, nan
			if w, ok := p.vals["hr_20m_w"]; ok {
				e.Best20W = w
			}
		}
	}
	slices.SortStableFunc(top, func(a, b hrPoint) int { return cmp.Compare(b.vals["hr_max10"], a.vals["hr_max10"]) })
	for i := 0; i+1 < len(top); i++ {
		hi, lo := top[i].vals["hr_max10"], top[i+1].vals["hr_max10"]
		if hi-lo <= maxConfirmBPM {
			e.MaxHR, e.MaxAt, e.ConfirmHR, e.ConfirmAt = hi, top[i].ref, lo, top[i+1].ref
			break
		}
	}
	if !math.IsNaN(e.Best20) {
		e.LTHR = lthrShare * e.Best20
	}
	return e
}

// calcHRZones calculates family f's zones from the stored entries of the
// window before now; withHR is the family's activities with heart rate as
// last listed. False when there is nothing to tell.
func calcHRZones(f hrFamily, acts map[string]storedMetrics, now time.Time, withHR int) (storedHRZones, bool) {
	from := now.AddDate(0, 0, -hrWindowDays)
	fromS := from.Format("2006-01-02T15:04:05")
	var pts []hrPoint
	for k, sm := range acts {
		if sm.Rev < entryRev || sm.Start < fromS || !slices.Contains(f.sports, sm.Sport) {
			continue
		}
		if _, ok := sm.Vals["hr_max10"]; !ok {
			continue
		}
		id, _ := strconv.ParseInt(k, 10, 64)
		pts = append(pts, hrPoint{ref: hrRef{ID: id, Start: sm.Start, Name: sm.Name, Sport: sm.Sport}, vals: sm.Vals})
	}
	// Newest first: ties go the same way on every run.
	slices.SortFunc(pts, func(a, b hrPoint) int {
		if c := strings.Compare(b.ref.Start, a.ref.Start); c != 0 {
			return c
		}
		return cmp.Compare(b.ref.ID, a.ref.ID)
	})
	if len(pts) == 0 && withHR == 0 {
		return storedHRZones{}, false
	}
	e := estimateHR(pts)
	z := storedHRZones{CalculatedAt: now.UTC(), From: from.Format("2006-01-02"), WithHR: withHR, Used: len(pts)}
	if finite(e.MaxHR) {
		z.MaxHR, z.ConfirmHR = e.MaxHR, e.ConfirmHR
		z.MaxAt, z.ConfirmAt = &e.MaxAt, &e.ConfirmAt
	}
	if finite(e.LTHR) {
		z.Best20, z.Best20At = e.Best20, &e.Best20At
		z.LTHR = math.Round(e.LTHR*10) / 10
		z.Zones = f.zones(e.LTHR)
		if finite(e.Best20W) {
			z.Best20W = e.Best20W
		}
	}
	return z, true
}

// hardestFirst orders the activities still to read: the highest max heart
// rate and the highest average over 20 min or more take turns, so the few
// streams the budget allows go where the max and the LTHR are.
func hardestFirst(acts []summaryActivity) []summaryActivity {
	byMax := slices.Clone(acts)
	slices.SortStableFunc(byMax, func(a, b summaryActivity) int { return cmp.Compare(b.MaxHeartrate, a.MaxHeartrate) })
	var byAvg []summaryActivity
	for _, x := range acts {
		if x.MovingTime >= 1200 {
			byAvg = append(byAvg, x)
		}
	}
	slices.SortStableFunc(byAvg, func(a, b summaryActivity) int { return cmp.Compare(b.AverageHeartrate, a.AverageHeartrate) })
	seen := map[int64]bool{}
	return slices.DeleteFunc(interleave([][]summaryActivity{byMax, byAvg}), func(x summaryActivity) bool {
		dup := seen[x.ID]
		seen[x.ID] = true
		return dup
	})
}

// dropEntries removes keys from m and recalculates the zones of their
// sports; gone (deleted on Strava) also counts them out of WithHR. False
// when none was there.
func dropEntries(m *metricFile, keys []string, gone bool, now time.Time) bool {
	touched := map[string]bool{}
	less := map[string]int{}
	for _, k := range keys {
		sm, ok := m.Activities[k]
		if !ok {
			continue
		}
		touched[sm.Sport] = true
		if f, ok := familyOf(sm.Sport); ok && gone {
			if _, hr := sm.Vals["hr_max10"]; hr {
				less[f.key]++
			}
		}
		delete(m.Activities, k)
	}
	if len(touched) == 0 {
		return false
	}
	withHR := map[string]int{}
	for key, n := range less {
		if z, ok := m.HRZones[key]; ok {
			withHR[key] = max(0, z.WithHR-n)
		}
	}
	recalcHRZones(m, touched, nil, withHR, now)
	return true
}

func (t *tools) hrZones(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		Sport string `json:"sport"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("hr_zones: " + err.Error())
	}
	fams := hrFamilies
	if args.Sport != "" {
		i := slices.IndexFunc(hrFamilies, func(f hrFamily) bool { return f.key == args.Sport })
		if i < 0 {
			return mcpserver.ErrorResult("hr_zones: sport must be cycling or running")
		}
		fams = hrFamilies[i : i+1]
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	now := t.svc.now()
	from := now.AddDate(0, 0, -hrWindowDays)
	// The index, synced back to the window's start (padded: the window is
	// compared in wall-clock time below); more says it does not reach back
	// that far yet.
	all, more, err := t.svc.listActivities(ctx, user, from.Unix()-dayPadS, 0, maxBackfillPages)
	if err != nil {
		return failure(err)
	}
	// One comparison for the window everywhere: wall-clock strings, as
	// stored entries and calcHRZones have them.
	fromS := from.Format("2006-01-02T15:04:05")
	inWindow := func(x summaryActivity) bool { return x.localStart().Format("2006-01-02T15:04:05") >= fromS }
	var list []summaryActivity
	for _, x := range all {
		if inWindow(x) {
			list = append(list, x)
		}
	}
	stored := t.loadMetrics(user, a.ID)
	known := func(x summaryActivity) bool {
		sm, ok := stored[strconv.FormatInt(x.ID, 10)]
		return ok && sm.Rev >= entryRev
	}

	// Per family: its activities with heart rate, and the hardest of those
	// not read yet first. An entry stored before the HR keys existed is
	// read again: its other values come back the same.
	acts := make([][]summaryActivity, len(fams))
	queues := make([][]summaryActivity, len(fams))
	withHR := map[string]int{}
	for i, f := range fams {
		var need []summaryActivity
		for _, x := range list {
			if fx, ok := familyOf(x.SportType); !ok || fx.key != f.key || !x.HasHeartrate || x.Manual {
				continue
			}
			acts[i] = append(acts[i], x)
			if !known(x) {
				need = append(need, x)
			}
		}
		queues[i] = hardestFirst(need)
		withHR[f.key] = len(acts[i])
	}
	read := t.readStreams(ctx, user, interleave(queues))
	added := map[string]storedMetrics{}
	for id, sm := range read.got {
		added[strconv.FormatInt(id, 10)] = sm
	}
	// Sport and name of the listed entries are set under the file's lock,
	// on the entry as it is then: never from the snapshot above.
	asked := make([]string, 0, len(fams))
	for _, f := range fams {
		asked = append(asked, f.key)
	}
	u := metricsUpdate{add: added, listed: list, withHR: withHR, recalc: asked}
	zones, saved := t.updateMetrics(user, a.ID, u)
	if zones == nil {
		// Disconnected or another athlete meanwhile: the same update on the
		// snapshot, unsaved, so this answer still gets its zones.
		m := metricFile{Activities: stored}
		applyUpdate(&m, u, now)
		zones = m.HRZones
	}
	keep := "calculated %s and stored: profile and activity_analysis use them while Strava's zones are its defaults, " +
		"and an analysed activity that moves them recalculates them."
	if !saved {
		keep = "calculated %s, not stored this time (disconnected meanwhile, or the write failed)."
	}

	var b strings.Builder
	var sources []mcpserver.Source
	cited := map[int64]int{}
	cite := func(r *hrRef) string {
		i, ok := cited[r.ID]
		if !ok {
			i = len(sources)
			cited[r.ID] = i
			sources = append(sources, r.source())
		}
		return marker(i) + " " + r.date() + " · " + r.Name
	}
	fmt.Fprintf(&b, "Heart-rate zones from the user's own streams, %s – %s, "+keep+" Max heart rate: the best 10 s, confirmed by a "+
		"second activity within %d bpm. Threshold heart rate (LTHR): %.0f %% of the best 20 min. Zones: Friel's, from the LTHR.\n",
		from.Format("2006-01-02"), now.Format("2006-01-02"), now.UTC().Format("2006-01-02 15:04 UTC"), maxConfirmBPM, lthrShare*100)
	retry := false
	for i, f := range fams {
		notTried, failed := 0, 0
		for _, x := range acts[i] {
			switch {
			case known(x) || read.got[x.ID].Rev >= entryRev:
			case read.failed[x.ID]:
				failed++
			default:
				notTried++
			}
		}
		fmt.Fprintf(&b, "%s (%s): %d with heart rate, %d read", f.key, strings.Join(f.sports, ", "), len(acts[i]), len(acts[i])-notTried-failed)
		if notTried > 0 {
			fmt.Fprintf(&b, "; %d not read yet (%s)", notTried, read.stoppedBy)
		}
		if failed > 0 {
			fmt.Fprintf(&b, "; %d failed to read (a later call retries)", failed)
		}
		retry = retry || notTried > 0 || failed > 0
		b.WriteByte('\n')
		z, ok := zones[f.key]
		if !ok || z.Used == 0 {
			continue
		}
		if z.MaxAt == nil {
			fmt.Fprintf(&b, "  max heart rate: needs two activities within %d bpm at the top; not found\n", maxConfirmBPM)
		} else {
			fmt.Fprintf(&b, "  max heart rate: %s, best 10 s in %s; %s within %d bpm (%s)\n",
				bpm(z.MaxHR), cite(z.MaxAt), cite(z.ConfirmAt), maxConfirmBPM, bpm(z.ConfirmHR))
		}
		if z.Best20At == nil {
			b.WriteString("  threshold heart rate: no activity with 20 min of heart rate read\n")
			continue
		}
		fmt.Fprintf(&b, "  threshold heart rate (LTHR): %s, %.0f %% of the best 20 min (%s) in %s",
			bpm(z.LTHR), lthrShare*100, bpm(z.Best20), cite(z.Best20At))
		switch {
		// Today's FTP says little about a ride months ago: only a recent
		// one is judged by it. Coasting counts as 0 W in that average.
		case f.key == "cycling" && a.FTP > 0 && z.Best20W > 0 && z.Best20At.Start >= now.AddDate(0, 0, -ftpRecentDays).Format("2006-01-02T15:04:05"):
			share := z.Best20W / a.FTP
			fmt.Fprintf(&b, "; those 20 min averaged %.0f W (coasting as 0 W), %.0f %% of today's FTP", z.Best20W, share*100)
			if share >= thresholdFTP {
				b.WriteString(": a threshold effort")
			} else {
				b.WriteString(": below threshold, so the LTHR may be low")
			}
		case z.MaxHR > 0 && z.Best20 < lowEffortShare*z.MaxHR:
			fmt.Fprintf(&b, "; under %.0f %% of the max heart rate: no threshold effort in the window, so the LTHR is likely low", lowEffortShare*100)
		}
		b.WriteString("\n  zones: " + zoneList(z.Zones, " bpm") + "\n")
	}
	if more {
		b.WriteString("Not every activity of the window is listed yet (the list is read back within the rate budget): older ones are left out; a later call lists more.\n")
	}
	if retry {
		b.WriteString("Call hr_zones again later to read more.\n")
	}

	switch z, zerr := t.svc.zones(ctx, user); {
	case zerr != nil:
		fmt.Fprintf(&b, "Strava's heart-rate zones: unavailable (%s)\n", failure(zerr).Text)
	case z.HeartRate == nil || len(z.HeartRate.Zones) == 0:
		b.WriteString("Strava sent no heart-rate zones.\n")
	case z.HeartRate.CustomZones:
		fmt.Fprintf(&b, "Strava's heart-rate zones (custom): %s; activity_analysis uses these.\n", zoneList(z.HeartRate.Zones, " bpm"))
	default:
		fmt.Fprintf(&b, "Strava's heart-rate zones (Strava's defaults, usually age-based): %s; activity_analysis uses the "+
			"zones above instead where a sport has them. Custom zones set in Strava replace both.\n", zoneList(z.HeartRate.Zones, " bpm"))
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

// ownZonesLine is the profile's line for one family's stored zones.
func ownZonesLine(key string, z storedHRZones) string {
	s := fmt.Sprintf("heart-rate zones from the user's own streams (%s, calculated %s", key, z.CalculatedAt.Format("2006-01-02"))
	if z.MaxHR > 0 {
		s += ", max " + bpm(z.MaxHR)
	}
	return s + fmt.Sprintf(", LTHR %s): %s\n", bpm(z.LTHR), zoneList(z.Zones, " bpm"))
}

// interleave takes one from each queue in turn, so a budget stop leaves
// every family some reads.
func interleave(queues [][]summaryActivity) []summaryActivity {
	var out []summaryActivity
	for i := 0; ; i++ {
		took := false
		for _, q := range queues {
			if i < len(q) {
				out, took = append(out, q[i]), true
			}
		}
		if !took {
			return out
		}
	}
}
