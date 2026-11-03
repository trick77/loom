package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

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

// hrPoint is one activity's stored values.
type hrPoint struct {
	act  summaryActivity
	vals map[string]float64
}

// hrEstimate is NaN where the activities do not tell.
type hrEstimate struct {
	MaxHR, ConfirmHR float64
	MaxAt, ConfirmAt summaryActivity
	Best20, Best20W  float64 // the best 20 min mean HR and that activity's best 20 min power
	Best20At         summaryActivity
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
			e.Best20, e.Best20At, e.Best20W = v, p.act, nan
			if w, ok := p.vals["mm_1200"]; ok {
				e.Best20W = w
			}
		}
	}
	slices.SortStableFunc(top, func(a, b hrPoint) int { return cmp.Compare(b.vals["hr_max10"], a.vals["hr_max10"]) })
	for i := 0; i+1 < len(top); i++ {
		hi, lo := top[i].vals["hr_max10"], top[i+1].vals["hr_max10"]
		if hi-lo <= maxConfirmBPM {
			e.MaxHR, e.MaxAt, e.ConfirmHR, e.ConfirmAt = hi, top[i].act, lo, top[i+1].act
			break
		}
	}
	if !math.IsNaN(e.Best20) {
		e.LTHR = lthrShare * e.Best20
	}
	return e
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
	out := make([]summaryActivity, 0, len(acts))
	seen := map[int64]bool{}
	for i := 0; i < len(byMax) || i < len(byAvg); i++ {
		for _, l := range [][]summaryActivity{byMax, byAvg} {
			if i < len(l) && !seen[l[i].ID] {
				seen[l[i].ID] = true
				out = append(out, l[i])
			}
		}
	}
	return out
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
	list, more, err := t.svc.listActivities(ctx, user, from.Unix(), 0, maxListPages)
	if err != nil {
		return failure(err)
	}
	stored := t.loadMetrics(user, a.ID)
	known := func(x summaryActivity) (storedMetrics, bool) {
		sm, ok := stored[strconv.FormatInt(x.ID, 10)]
		_, has := sm.Vals["hr_max10"]
		return sm, ok && has
	}

	// Per family: its activities with heart rate, and the hardest of those
	// not read yet first. An entry stored before the HR keys existed is
	// read again: its other values come back the same.
	acts := make([][]summaryActivity, len(fams))
	queues := make([][]summaryActivity, len(fams))
	for i, f := range fams {
		var need []summaryActivity
		for _, x := range list {
			if fx, ok := familyOf(x.SportType); !ok || fx.key != f.key || !x.HasHeartrate || x.Manual {
				continue
			}
			acts[i] = append(acts[i], x)
			if _, ok := known(x); !ok {
				need = append(need, x)
			}
		}
		queues[i] = hardestFirst(need)
	}
	added, stoppedBy := t.readHR(ctx, user, queues)
	t.storeMetrics(user, a.ID, added, stored)
	for k, v := range added {
		stored[k] = v
	}

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
	fmt.Fprintf(&b, "Heart-rate zones from the user's own streams, %s – %s. Max heart rate: the best 10 s, confirmed by a "+
		"second activity within %d bpm. Threshold heart rate (LTHR): %.0f %% of the best 20 min. Zones: Friel's, from the LTHR.\n",
		from.Format("2006-01-02"), now.Format("2006-01-02"), maxConfirmBPM, lthrShare*100)
	unread := false
	for i, f := range fams {
		var pts []hrPoint
		for _, x := range acts[i] {
			if sm, ok := known(x); ok {
				pts = append(pts, hrPoint{act: x, vals: sm.Vals})
			}
		}
		fmt.Fprintf(&b, "%s (%s): %d with heart rate, %d read", f.key, strings.Join(f.sports, ", "), len(acts[i]), len(pts))
		if n := len(acts[i]) - len(pts); n > 0 {
			unread = true
			why := stoppedBy
			if why == "" {
				why = "Strava sent no heart-rate stream"
			}
			fmt.Fprintf(&b, "; %d not read yet (%s)", n, why)
		}
		b.WriteByte('\n')
		if len(pts) == 0 {
			continue
		}
		e := estimateHR(pts)
		if math.IsNaN(e.MaxHR) {
			fmt.Fprintf(&b, "  max heart rate: needs two activities within %d bpm at the top; not found\n", maxConfirmBPM)
		} else {
			fmt.Fprintf(&b, "  max heart rate: %s, best 10 s in %s; %s within %d bpm (%s)\n",
				bpm(e.MaxHR), cite(e.MaxAt), cite(e.ConfirmAt), maxConfirmBPM, bpm(e.ConfirmHR))
		}
		if math.IsNaN(e.LTHR) {
			b.WriteString("  threshold heart rate: no activity with 20 min of heart rate read\n")
			continue
		}
		fmt.Fprintf(&b, "  threshold heart rate (LTHR): %s, %.0f %% of the best 20 min (%s) in %s",
			bpm(e.LTHR), lthrShare*100, bpm(e.Best20), cite(e.Best20At))
		switch {
		case f.key == "cycling" && a.FTP > 0 && finite(e.Best20W):
			share := e.Best20W / a.FTP
			fmt.Fprintf(&b, "; those 20 min averaged %.0f W, %.0f %% of FTP", e.Best20W, share*100)
			if share >= thresholdFTP {
				b.WriteString(": a threshold effort")
			} else {
				b.WriteString(": below threshold, so the LTHR is likely low")
			}
		case finite(e.MaxHR) && e.Best20 < lowEffortShare*e.MaxHR:
			fmt.Fprintf(&b, "; under %.0f %% of the max heart rate: no threshold effort in the window, so the LTHR is likely low", lowEffortShare*100)
		}
		b.WriteString("\n  zones: " + zoneList(f.zones(e.LTHR), " bpm") + "\n")
	}
	if more {
		fmt.Fprintf(&b, "The list stopped at %d activities: older ones in the window are left out.\n", maxListPages*activitiesPerPage)
	}
	if unread && stoppedBy != "" {
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
		fmt.Fprintf(&b, "Strava's heart-rate zones (Strava's defaults, usually age-based): %s; activity_analysis uses these. "+
			"Custom zones set in Strava (e.g. from the estimate above) replace them in every analysis.\n", zoneList(z.HeartRate.Zones, " bpm"))
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

// readHR reads the streams of the queued activities, one family after the
// other in turns, within the rate and time budget; stoppedBy says why it
// stopped early.
func (t *tools) readHR(ctx context.Context, user string, queues [][]summaryActivity) (map[string]storedMetrics, string) {
	added := map[string]storedMetrics{}
	fetched := 0
	for i := 0; ; i++ {
		progressed := false
		for _, q := range queues {
			if i >= len(q) {
				continue
			}
			progressed = true
			if reason := t.budgetLeft(ctx, fetched); reason != "" {
				return added, reason
			}
			x := q[i]
			sctx, cancel := context.WithTimeout(ctx, streamTimeout)
			s, err := t.svc.streams(sctx, user, x.ID)
			cancel()
			fetched++
			var rl *rateLimitedError
			if errors.As(err, &rl) {
				return added, "the Strava rate budget"
			}
			if err != nil {
				continue
			}
			added[strconv.FormatInt(x.ID, 10)] = storedFrom(computeMetrics(s, 0, s.n, nil, nil), x.localStart())
		}
		if !progressed {
			return added, ""
		}
	}
}
