package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/mcpserver"
)

const (
	defaultListLimit = 30
	maxListLimit     = 500
	// maxListPages is how many 200-activity pages one call reads.
	maxListPages = 3
	// dayPadS widens a local-date window into the UTC epochs Strava filters
	// on: no time zone is further than 14 h from UTC. The local dates then
	// filter exactly.
	dayPadS = 14 * 3600
	// maxGearDetails bounds the /gear/{id} reads of one call.
	maxGearDetails = 10
)

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t, err == nil
}

func (t *tools) listActivities(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		After       string `json:"after"`
		Before      string `json:"before"`
		SportType   string `json:"sport_type"`
		Limit       int    `json:"limit"`
		TrainerOnly bool   `json:"trainer_only"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("list_activities: " + err.Error())
	}
	var after, before time.Time
	if args.After != "" {
		var ok bool
		if after, ok = parseDay(args.After); !ok {
			return mcpserver.ErrorResult("after must be YYYY-MM-DD")
		}
	}
	if args.Before != "" {
		var ok bool
		if before, ok = parseDay(args.Before); !ok {
			return mcpserver.ErrorResult("before must be YYYY-MM-DD")
		}
	}
	if !after.IsZero() && !before.IsZero() && !after.Before(before) {
		return mcpserver.ErrorResult("after must be earlier than before")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	limit = min(limit, maxListLimit)
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	var afterE, beforeE int64
	if !after.IsZero() {
		afterE = after.Unix() - dayPadS
	}
	if !before.IsZero() {
		beforeE = before.Unix() + dayPadS
	}
	// Newest first without after: as many pages as the limit needs, more
	// when a filter thins them. With after Strava answers oldest first, so
	// all pages are read and sorted here.
	pages := (limit + activitiesPerPage - 1) / activitiesPerPage
	if !after.IsZero() || args.SportType != "" || args.TrainerOnly {
		pages = maxListPages
	}
	list, more, err := t.svc.listActivities(ctx, user, afterE, beforeE, min(pages, maxListPages))
	if err != nil {
		return failure(err)
	}
	var rows []summaryActivity
	for _, x := range list {
		ls := x.localStart()
		switch {
		case !after.IsZero() && ls.Before(after):
		case !before.IsZero() && !ls.Before(before):
		case args.SportType != "" && !strings.EqualFold(x.SportType, args.SportType):
		case args.TrainerOnly && !x.indoor():
		default:
			rows = append(rows, x)
		}
	}
	slices.SortStableFunc(rows, func(p, q summaryActivity) int { return q.localStart().Compare(p.localStart()) })
	matched := len(rows)
	rows = rows[:min(len(rows), limit)]
	u := unitsOf(a)
	if len(rows) == 0 {
		return mcpserver.Result{Text: "No Strava activities match" + windowText(args.After, args.Before, args.SportType, args.TrainerOnly) + "."}
	}
	gearNames := map[string]string{}
	for _, g := range append(slices.Clone(a.Bikes), a.Shoes...) {
		gearNames[g.ID] = g.Name
	}
	stored := t.loadMetrics(user, a.ID)

	var b strings.Builder
	fmt.Fprintf(&b, "Strava activities%s, newest first: %d", windowText(args.After, args.Before, args.SportType, args.TrainerOnly), len(rows))
	// The count is only exact when every page was read. A full last page
	// matters when it left the list short, or with after: Strava then
	// answers oldest first, so the newest are past the pages read.
	if !more && matched > len(rows) {
		fmt.Fprintf(&b, " of %d (limit %d)", matched, limit)
	}
	if more && (len(rows) < limit || !after.IsZero()) {
		fmt.Fprintf(&b, "; one call reads at most %d activities, there are more: narrow the dates", maxListPages*activitiesPerPage)
	}
	b.WriteString("\n")
	sources := []mcpserver.Source{}
	shown := 0
	for i, x := range rows {
		line := marker(i) + " " + activityRow(x, u, gearNames) + "\n"
		if b.Len()+len(line) > textBudget*3/4 {
			break
		}
		b.WriteString(line)
		shown++
		if i < maxSources {
			sources = append(sources, activitySource(x))
		}
	}
	if shown < len(rows) {
		fmt.Fprintf(&b, "… %d more rows not shown here; all are in strava_activities.csv\n", len(rows)-shown)
	}

	// Totals per sport, then per week (or month, for a long window).
	type agg struct {
		n               int
		dist, mov, gain float64
	}
	bySport := map[string]*agg{}
	var sports []string
	for _, x := range rows {
		g, ok := bySport[x.SportType]
		if !ok {
			g = &agg{}
			bySport[x.SportType] = g
			sports = append(sports, x.SportType)
		}
		g.n++
		g.dist += x.Distance
		g.mov += float64(x.MovingTime)
		g.gain += x.TotalElevationGain
	}
	parts := make([]string, 0, len(sports))
	for _, s := range sports {
		g := bySport[s]
		parts = append(parts, fmt.Sprintf("%s %d · %s · %s · %s", s, g.n, u.dist(g.dist), clock(g.mov), u.elev(g.gain)))
	}
	b.WriteString("totals: " + strings.Join(parts, " | ") + "\n")

	first, last := rows[len(rows)-1].localStart(), rows[0].localStart()
	monthly := last.Sub(first) > 26*7*24*time.Hour
	periods := map[string]*agg{}
	var order []string
	for i := len(rows) - 1; i >= 0; i-- {
		ls := rows[i].localStart()
		key := isoWeek(weekStart(ls)) + " (" + weekStart(ls).Format("Jan 2") + ")"
		if monthly {
			key = ls.Format("2006-01")
		}
		g, ok := periods[key]
		if !ok {
			g = &agg{}
			periods[key] = g
			order = append(order, key)
		}
		g.n++
		g.dist += rows[i].Distance
		g.mov += float64(rows[i].MovingTime)
		g.gain += rows[i].TotalElevationGain
	}
	if len(order) > 1 {
		kind := "weeks"
		if monthly {
			kind = "months"
		}
		parts = parts[:0]
		for _, k := range order {
			g := periods[k]
			parts = append(parts, fmt.Sprintf("%s %d · %s · %s", k, g.n, u.dist(g.dist), clock(g.mov)))
		}
		b.WriteString(kind + ": " + strings.Join(parts, " | ") + "\n")
	}

	acts := newCSV(hdrActivities)
	weeks := newWeekTable(first, last, nil)
	for _, x := range rows {
		acts.row(strconv.FormatInt(x.ID, 10), x.localStart().Format("2006-01-02"), x.Name, x.SportType, boolCell(x.indoor()),
			cell(x.Distance/1000, 2), intCell(x.MovingTime), cell(x.TotalElevationGain, 0), cell(x.AverageSpeed*3.6, 1),
			optCell(x.AverageHeartrate), optCell(x.AverageWatts), optCell(x.WeightedAverageWatts),
			optCell(x.Kilojoules), optCell(x.SufferScore))
		sm, ok := stored[strconv.FormatInt(x.ID, 10)]
		weeks.add(x, activityTSS(x, sm, ok, a.FTP), nan)
	}
	files := []mcpserver.File{acts.file("strava_activities.csv"), weeks.file()}
	b.WriteString(fileNames(files))
	return mcpserver.Result{Text: b.String(), Sources: sources, Files: files}
}

// optCell is v rounded, empty when Strava sent none (zero).
func optCell(v float64) string {
	if v == 0 {
		return ""
	}
	return cell(v, 0)
}

func windowText(after, before, sport string, trainer bool) string {
	var parts []string
	if sport != "" {
		parts = append(parts, sport)
	}
	if trainer {
		parts = append(parts, "indoor only")
	}
	switch {
	case after != "" && before != "":
		parts = append(parts, after+" to before "+before)
	case after != "":
		parts = append(parts, "since "+after)
	case before != "":
		parts = append(parts, "before "+before)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// activityRow is one compact list line.
func activityRow(x summaryActivity, u units, gearNames map[string]string) string {
	parts := []string{day(x.localStart()), x.Name, x.SportType}
	if x.indoor() {
		parts[2] += " (indoor)"
	}
	if x.Distance > 0 {
		parts = append(parts, u.dist(x.Distance))
	}
	t := clock(float64(x.MovingTime))
	if x.ElapsedTime > x.MovingTime+60 {
		t += " (elapsed " + clock(float64(x.ElapsedTime)) + ")"
	}
	parts = append(parts, t)
	if x.TotalElevationGain > 0 {
		parts = append(parts, u.elev(x.TotalElevationGain))
	}
	if x.AverageSpeed > 0 {
		parts = append(parts, u.tempo(x.AverageSpeed, x.SportType))
	}
	if x.HasHeartrate && x.AverageHeartrate > 0 {
		parts = append(parts, fmt.Sprintf("HR %.0f/%.0f", x.AverageHeartrate, x.MaxHeartrate))
	}
	if x.AverageWatts > 0 {
		p := fmt.Sprintf("%.0f W", x.AverageWatts)
		if x.WeightedAverageWatts > 0 {
			p += fmt.Sprintf(" (weighted %.0f W)", x.WeightedAverageWatts)
		}
		parts = append(parts, p)
	}
	if x.Kilojoules > 0 {
		parts = append(parts, thousands(x.Kilojoules)+" kJ")
	}
	if x.SufferScore > 0 {
		parts = append(parts, fmt.Sprintf("RE %.0f", x.SufferScore))
	}
	if x.PRCount > 0 {
		parts = append(parts, plural(x.PRCount, "PR", "PRs"))
	}
	if g := gearNames[x.GearID]; g != "" {
		parts = append(parts, g)
	}
	if x.KudosCount > 0 {
		parts = append(parts, plural(x.KudosCount, "kudo", "kudos"))
	}
	if x.CommentCount > 0 {
		parts = append(parts, plural(x.CommentCount, "comment", "comments"))
	}
	parts = append(parts, fmt.Sprintf("id %d", x.ID))
	return strings.Join(parts, " · ")
}

// activityTSS is one activity's TSS: from its stored stream NP when known,
// else from Strava's weighted average power (its own NP-like figure, from a
// power meter only); NaN without power or FTP.
func activityTSS(x summaryActivity, sm storedMetrics, ok bool, ftp float64) float64 {
	if ok {
		if np, has := sm.Vals["np"]; has {
			return tss(sm.MovingS, np, ftp)
		}
	}
	if x.DeviceWatts && x.WeightedAverageWatts > 0 {
		return tss(x.MovingTime, x.WeightedAverageWatts, ftp)
	}
	return nan
}

// loadMetrics is the user's metric store for athleteID; empty when
// unreadable or kept for another athlete.
func (t *tools) loadMetrics(user string, athleteID int64) map[string]storedMetrics {
	sl := t.svc.slot(user)
	sl.dataMu.Lock()
	defer sl.dataMu.Unlock()
	m, _ := t.svc.store.loadMetrics(user)
	if m.Athlete != 0 && m.Athlete != athleteID {
		return map[string]storedMetrics{}
	}
	return m.Activities
}

var frameTypes = map[int]string{1: "mountain bike", 2: "cross", 3: "road", 4: "time trial", 5: "gravel"}

func (t *tools) gear(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		GearID string `json:"gear_id"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("gear: " + err.Error())
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	u := unitsOf(a)
	own := append(slices.Clone(a.Bikes), a.Shoes...)
	if id := strings.TrimSpace(args.GearID); id != "" {
		if !slices.ContainsFunc(own, func(g summaryGear) bool { return g.ID == id }) {
			return mcpserver.ErrorResult(fmt.Sprintf("Gear %q is not one of the user's bikes or shoes; profile lists them.", id))
		}
		own = slices.DeleteFunc(own, func(g summaryGear) bool { return g.ID != id })
	}
	if len(own) == 0 {
		return mcpserver.Result{Text: "The user has no bikes or shoes on Strava."}
	}
	var b strings.Builder
	b.WriteString("Strava gear:\n")
	for i, g := range own {
		line := fmt.Sprintf("- %s (id %s) · %s", g.Name, g.ID, u.dist(g.Distance))
		if i < maxGearDetails {
			if d, err := t.svc.gear(ctx, user, g.ID); err == nil {
				if bm := strings.TrimSpace(d.BrandName + " " + d.ModelName); bm != "" {
					line += " · " + bm
				}
				if ft := frameTypes[d.FrameType]; ft != "" {
					line += " · " + ft
				}
				if s := mcpserver.OneLine(d.Description, 160); s != "" {
					line += " · " + s
				}
			}
		}
		if g.Primary {
			line += " · primary"
		}
		if g.Retired {
			line += " · retired"
		}
		b.WriteString(line + "\n")
	}
	return mcpserver.Result{Text: b.String()}
}

func routeURL(r route) string { return "https://www.strava.com/routes/" + r.idString() }

func (t *tools) routes(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		RouteID json.RawMessage `json:"route_id"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("routes: " + err.Error())
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	u := unitsOf(a)
	if len(args.RouteID) > 0 && string(args.RouteID) != "null" {
		id, ok := parseID(args.RouteID)
		if !ok {
			return mcpserver.ErrorResult("route_id must be a route id")
		}
		var r struct {
			route
			Athlete struct {
				ID int64 `json:"id"`
			} `json:"athlete"`
		}
		key := "route/" + strconv.FormatInt(id, 10)
		got, err := cached[json.RawMessage](ctx, t.svc, user, t.svc.cache(user).profile, key, "/routes/"+strconv.FormatInt(id, 10), nil)
		if err == nil {
			err = json.Unmarshal(got, &r)
		}
		if err != nil {
			return failure(err)
		}
		if r.Athlete.ID != 0 && r.Athlete.ID != a.ID {
			return mcpserver.ErrorResult("That route is not one of the user's own routes.")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "[1] Route %s · %s · %s · %s", r.idString(), r.Name, u.dist(r.Distance), u.elev(r.ElevationGain))
		if r.EstimatedMovingTime > 0 {
			b.WriteString(" · est. " + clock(r.EstimatedMovingTime))
		}
		b.WriteByte('\n')
		if s := mcpserver.OneLine(r.Description, 400); s != "" {
			b.WriteString(s + "\n")
		}
		if len(r.Segments) > 0 {
			fmt.Fprintf(&b, "segments on it (%d):\n", len(r.Segments))
			for i, s := range r.Segments {
				if i == maxSegments {
					fmt.Fprintf(&b, "  … %d more\n", len(r.Segments)-maxSegments)
					break
				}
				fmt.Fprintf(&b, "  %s · %s · %.1f %% · segment %d\n", s.Name, u.dist(s.Distance), s.AverageGrade, s.ID)
			}
		}
		return mcpserver.Result{Text: b.String(), Sources: []mcpserver.Source{{URL: routeURL(r.route), Title: r.Name, Label: label, Snippet: "Strava route"}}}
	}
	list, err := cached[[]route](ctx, t.svc, user, t.svc.cache(user).profile, "routes",
		fmt.Sprintf("/athletes/%d/routes", a.ID), url.Values{"per_page": {"100"}})
	if err != nil {
		return failure(err)
	}
	if len(list) == 0 {
		return mcpserver.Result{Text: "The user has no routes on Strava."}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Strava routes (%d):\n", len(list))
	sources := []mcpserver.Source{}
	for i, r := range list {
		kind := "ride"
		if r.Type == 2 {
			kind = "run"
		}
		fmt.Fprintf(&b, "%s %s · %s · %s · %s", marker(i), r.Name, kind, u.dist(r.Distance), u.elev(r.ElevationGain))
		if r.EstimatedMovingTime > 0 {
			b.WriteString(" · est. " + clock(r.EstimatedMovingTime))
		}
		if r.Starred {
			b.WriteString(" · starred")
		}
		fmt.Fprintf(&b, " · id %s\n", r.idString())
		if i < maxSources {
			sources = append(sources, mcpserver.Source{URL: routeURL(r), Title: r.Name, Label: label, Snippet: "Strava route"})
		}
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

func segmentURL(id int64) string { return fmt.Sprintf("https://www.strava.com/segments/%d", id) }

func (t *tools) segments(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		SegmentID json.RawMessage `json:"segment_id"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("segments: " + err.Error())
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	u := unitsOf(a)
	if len(args.SegmentID) > 0 && string(args.SegmentID) != "null" {
		id, ok := parseID(args.SegmentID)
		if !ok {
			return mcpserver.ErrorResult("segment_id must be a segment id")
		}
		return t.segment(ctx, user, u, id)
	}
	list, err := cached[[]summarySegment](ctx, t.svc, user, t.svc.cache(user).profile, "starred", "/segments/starred", url.Values{"per_page": {"100"}})
	if err != nil {
		return failure(err)
	}
	if len(list) == 0 {
		return mcpserver.Result{Text: "The user has no starred segments on Strava."}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Starred Strava segments (%d):\n", len(list))
	sources := []mcpserver.Source{}
	for i, s := range list {
		fmt.Fprintf(&b, "%s %s · %s · %s · %.1f %%", marker(i), s.Name, s.ActivityType, u.dist(s.Distance), s.AverageGrade)
		if s.PR != nil && s.PR.ElapsedTime > 0 {
			fmt.Fprintf(&b, " · PR %s", clock(float64(s.PR.ElapsedTime)))
			if s.PR.Date != "" {
				b.WriteString(" on " + shortDate(s.PR.Date))
			}
			if s.PR.EffortCount > 0 {
				fmt.Fprintf(&b, " · %s", plural(s.PR.EffortCount, "effort", "efforts"))
			}
		}
		fmt.Fprintf(&b, " · id %d\n", s.ID)
		if i < maxSources {
			sources = append(sources, mcpserver.Source{URL: segmentURL(s.ID), Title: s.Name, Label: label, Snippet: "Strava segment"})
		}
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

// maxSegmentEfforts is how many of the user's efforts on one segment show.
const maxSegmentEfforts = 50

func (t *tools) segment(ctx context.Context, user string, u units, id int64) mcpserver.Result {
	sid := strconv.FormatInt(id, 10)
	s, err := cached[summarySegment](ctx, t.svc, user, t.svc.cache(user).profile, "segment/"+sid, "/segments/"+sid, nil)
	if err != nil {
		return failure(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[1] Segment %d · %s · %s · %s · %.1f %% avg, %.1f %% max · %s", s.ID, s.Name, s.ActivityType,
		u.dist(s.Distance), s.AverageGrade, s.MaximumGrade, u.elev(s.ElevationHigh-s.ElevationLow))
	if loc := strings.Trim(s.City+", "+s.Country, ", "); loc != "" {
		b.WriteString(" · " + loc)
	}
	b.WriteByte('\n')
	pr := s.Stats
	if pr == nil && s.PR != nil {
		pr = &struct {
			ElapsedTime int    `json:"pr_elapsed_time"`
			Date        string `json:"pr_date"`
			EffortCount int    `json:"effort_count"`
		}{s.PR.ElapsedTime, s.PR.Date, s.PR.EffortCount}
	}
	if pr != nil && pr.ElapsedTime > 0 {
		fmt.Fprintf(&b, "the user's PR %s on %s · %s\n", clock(float64(pr.ElapsedTime)), shortDate(pr.Date), plural(pr.EffortCount, "effort", "efforts"))
	}
	sources := []mcpserver.Source{{URL: segmentURL(s.ID), Title: s.Name, Label: label, Snippet: "Strava segment"}}
	uc := t.svc.cache(user)
	efforts, err := summitOnly[[]segmentEffort](ctx, t.svc, user, uc.profile, uc.detail, "segment_efforts", "efforts/"+sid, "/segment_efforts",
		url.Values{"segment_id": {sid}, "per_page": {strconv.Itoa(maxSegmentEfforts)}})
	switch {
	case summitRefused(err):
		b.WriteString("The user's effort history on a segment " + msgSubscription + " (Summit); only the PR is shown.\n")
	case err != nil:
		b.WriteString("effort history unavailable: " + failure(err).Text + "\n")
	case len(efforts) == 0:
		b.WriteString("no efforts by the user on this segment\n")
	default:
		slices.SortStableFunc(efforts, func(p, q segmentEffort) int { return strings.Compare(q.StartDateLocal, p.StartDateLocal) })
		fmt.Fprintf(&b, "the user's efforts, newest first (%d):\n", len(efforts))
		for i, e := range efforts {
			fmt.Fprintf(&b, "%s %s · %s", marker(i+1), shortDate(e.StartDateLocal), clock(float64(e.ElapsedTime)))
			if e.AverageWatts > 0 {
				fmt.Fprintf(&b, " · %.0f W", e.AverageWatts)
			}
			if e.AverageHeartrate > 0 {
				fmt.Fprintf(&b, " · HR %.0f", e.AverageHeartrate)
			}
			if e.PRRank > 0 {
				fmt.Fprintf(&b, " · PR %s", ordinal(e.PRRank))
			}
			fmt.Fprintf(&b, " · activity %d\n", e.Activity.ID)
			if i+1 < maxSources {
				sources = append(sources, mcpserver.Source{URL: activityURL(e.Activity.ID), Title: s.Name + " · " + shortDate(e.StartDateLocal), Label: label, Snippet: "effort on " + s.Name})
			}
		}
	}
	return mcpserver.Result{Text: b.String(), Sources: sources}
}

// shortDate cuts an RFC 3339 time to its date.
func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// finite reports a known value.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
