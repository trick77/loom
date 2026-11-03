package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/mcpserver"
)

const (
	// maxSources is loom's cap on sources per tool result: items past it get
	// no [n] marker, since they could not be cited.
	maxSources = 20
	label      = "Strava"
	// textBudget keeps a tool's text under mcpserver's 24 KB cap with room
	// for the closing lines, so the cut is ours and says what is left out.
	textBudget = 20 << 10
)

const (
	msgNotConnected = "Strava is not connected: connect it in Settings → Strava."
	msgExpired      = "Strava sign-in expired: reconnect in Settings → Strava."
	msgPermission   = "Strava refused access: a permission is missing. Reconnect in Settings → Strava and keep every box ticked."
	msgSubscription = "needs a Strava subscription"
)

const instructions = "The user's own Strava data: profile, totals, activities, laps, streams, gear, routes and " +
	"starred segments. Read the numbers with these tools instead of estimating or guessing. Cite every activity " +
	"you mention with its [n]. For one ride or run use activity_analysis: it compares the activity with the " +
	"user's recent ones of the same sport. Give numbers with their units, as returned. When a chart would read " +
	"better (ride profile, power curve, weekly load, same-route gap), offer one and draw it with run_python from " +
	"the attached CSV files."

func activityURL(id int64) string { return fmt.Sprintf("https://www.strava.com/activities/%d", id) }

func activitySource(a summaryActivity) mcpserver.Source {
	return mcpserver.Source{URL: activityURL(a.ID), Title: a.Name, Label: label,
		Snippet: a.SportType + " · " + a.localStart().Format("2006-01-02")}
}

// marker numbers the first maxSources items for citation.
func marker(i int) string {
	if i < maxSources {
		return fmt.Sprintf("[%d]", i+1)
	}
	return "-"
}

func toolServer(svc *service, version string) mcpserver.Server {
	t := &tools{svc: svc}
	dateArg := func(what string) map[string]any {
		return map[string]any{"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`, "description": what + " (YYYY-MM-DD, the athlete's local date)."}
	}
	idArg := func(what string) map[string]any {
		return map[string]any{"type": []string{"integer", "string"}, "description": what}
	}
	return mcpserver.Server{
		Name:         "strava",
		Version:      version,
		Instructions: instructions,
		Tools: []mcpserver.Tool{
			{
				Name: "profile", Title: "My Strava profile",
				Description: "The user's Strava profile: name, FTP, weight, bikes and shoes with their distance, heart-rate and power zones.",
				Schema:      mcpserver.ObjectSchema(map[string]any{}),
				Run:         t.profile,
			},
			{
				Name: "totals", Title: "My Strava totals",
				Description: "Ride, run and swim totals: the last 4 weeks, this year and all time (count, distance, time, " +
					"elevation), plus the longest ride and biggest climb. Strava counts only activities visible to Everyone here.",
				Schema: mcpserver.ObjectSchema(map[string]any{}),
				Run:    t.totals,
			},
			{
				Name: "list_activities", Title: "List my Strava activities",
				Description: "The user's activities, newest first, one compact row each (date, name, sport, distance, " +
					"time, elevation, speed or pace, heart rate, power, kJ, relative effort, gear), with totals per sport " +
					"and per week or month. Attaches strava_activities.csv and strava_weeks.csv for run_python.",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"after":        dateArg("Only activities on or after this day"),
					"before":       dateArg("Only activities before this day"),
					"sport_type":   map[string]any{"type": "string", "description": "Only this Strava sport_type, e.g. Ride, VirtualRide, Run, Swim."},
					"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit, "description": "How many activities at most."},
					"trainer_only": map[string]any{"type": "boolean", "description": "Only indoor activities (trainer, Zwift)."},
				}),
				Run: t.listActivities,
			},
			{
				Name: "get_activity", Title: "Read a Strava activity",
				Description: "Everything Strava has on one of the user's activities: description, private note, device, " +
					"calories, perceived exertion, gear, laps, splits, best efforts, segment efforts with PR and KOM rank.",
				Schema: mcpserver.ObjectSchema(map[string]any{"id": idArg("The activity id, from list_activities.")}, "id"),
				Run:    t.getActivity,
			},
			{
				Name: "activity_analysis", Title: "Analyse a Strava activity",
				Description: "Analyse one activity from its streams: power (NP, IF, TSS, VI, mean-max curve, zones), " +
					"heart rate (zones, EF, decoupling), climbs, intervals, per-km rows, always compared with the user's " +
					"activities of the same sport in the weeks before (median, rank, best/worst in window, same route, " +
					"weekly load). Attaches CSV files for charts in run_python.",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"id":         idArg("The activity id, from list_activities."),
					"weeks":      map[string]any{"type": "integer", "minimum": 1, "maximum": maxWeeks, "default": defaultWeeks, "description": "How many weeks before the activity to compare with."},
					"mix_indoor": map[string]any{"type": "boolean", "description": "Compare indoor (VirtualRide) and outdoor (Ride) together. Default: kept apart."},
					"from_km":    map[string]any{"type": "number", "minimum": 0, "description": "Analyse only the section from this distance (km)."},
					"to_km":      map[string]any{"type": "number", "minimum": 0, "description": "Analyse only the section up to this distance (km)."},
				}, "id"),
				Run: t.activityAnalysis,
			},
			{
				Name: "compare_activities", Title: "Compare Strava activities",
				Description: "Side-by-side table of 2 to 5 of the user's activities: distance, time, speed, elevation, " +
					"heart rate, NP, IF, TSS, EF, decoupling, power bests. For progress on the same route or over time.",
				Schema: mcpserver.ObjectSchema(map[string]any{
					"ids": map[string]any{"type": "array", "minItems": 2, "maxItems": maxCompare, "items": map[string]any{"type": []string{"integer", "string"}},
						"description": "The activity ids, from list_activities."},
				}, "ids"),
				Run: t.compareActivities,
			},
			{
				Name: "gear", Title: "My Strava gear",
				Description: "The user's bikes and shoes: brand, model, frame type, total distance, retired or not. Without an id, all of them.",
				Schema:      mcpserver.ObjectSchema(map[string]any{"gear_id": map[string]any{"type": "string", "description": "One gear id (b… or g…), from profile."}}),
				Run:         t.gear,
			},
			{
				Name: "routes", Title: "My Strava routes",
				Description: "The user's own routes: distance, elevation, estimated moving time. With a route id, that route with the segments on it.",
				Schema:      mcpserver.ObjectSchema(map[string]any{"route_id": idArg("One route id, from the list.")}),
				Run:         t.routes,
			},
			{
				Name: "segments", Title: "My starred Strava segments",
				Description: "The user's starred segments with their PR and effort count. With a segment id, that segment " +
					"and the user's own effort history on it (the history needs a Strava subscription).",
				Schema: mcpserver.ObjectSchema(map[string]any{"segment_id": idArg("One segment id, from the list.")}),
				Run:    t.segments,
			},
		},
	}
}

type tools struct{ svc *service }

// failure turns a service error into the text the model reads.
func failure(err error) mcpserver.Result {
	var rl *rateLimitedError
	switch {
	case errors.Is(err, errNotConnected):
		return mcpserver.ErrorResult(msgNotConnected)
	case errors.Is(err, errExpired):
		return mcpserver.ErrorResult(msgExpired)
	case errors.Is(err, errPermission):
		return mcpserver.ErrorResult(msgPermission)
	case errors.As(err, &rl):
		return mcpserver.ErrorResult(rl.Error())
	case notFound(err):
		return mcpserver.ErrorResult("Strava has no such item among the user's own data.")
	case transient(err):
		return mcpserver.ErrorResult("Strava did not answer in time; try again.")
	default:
		return mcpserver.ErrorResult("Strava is unavailable right now: " + err.Error())
	}
}

// begin reads what every tool needs: the caller's id (the server checked it)
// and their athlete, whose units and FTP shape every answer.
func (t *tools) begin(ctx context.Context) (string, athlete, *mcpserver.Result) {
	user := mcpserver.User(ctx)
	if !validUserID(user) {
		r := mcpserver.ErrorResult("no loom user on this call")
		return "", athlete{}, &r
	}
	// Checked before any cache: a disconnected or expired user gets no
	// answer from data kept from before.
	if c, ok := t.svc.conn(user); !ok || c.Expired {
		r := failure(errNotConnected)
		if ok {
			r = failure(errExpired)
		}
		return "", athlete{}, &r
	}
	a, err := t.svc.athlete(ctx, user)
	if err != nil {
		r := failure(err)
		return "", athlete{}, &r
	}
	return user, a, nil
}

// parseID accepts an id as a JSON number or string.
func parseID(raw json.RawMessage) (int64, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return id, err == nil && id > 0
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		id, err := n.Int64()
		return id, err == nil && id > 0
	}
	return 0, false
}

func (t *tools) profile(ctx context.Context, _ json.RawMessage) mcpserver.Result {
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	z, zerr := t.svc.zones(ctx, user)
	u := unitsOf(a)
	var b strings.Builder
	fmt.Fprintf(&b, "[1] Strava profile: %s", a.name())
	if loc := strings.Trim(a.City+", "+a.Country, ", "); loc != "" {
		b.WriteString(" · " + loc)
	}
	if u.imperial {
		b.WriteString(" · units imperial")
	} else {
		b.WriteString(" · units metric")
	}
	if a.Summit {
		b.WriteString(" · Strava subscriber")
	}
	b.WriteByte('\n')
	parts := []string{}
	if a.FTP > 0 {
		parts = append(parts, fmt.Sprintf("FTP %.0f W", a.FTP))
	} else {
		parts = append(parts, "FTP not set (no IF, TSS or power zones)")
	}
	if a.Weight > 0 {
		if u.imperial {
			parts = append(parts, fmt.Sprintf("weight %.1f lb", a.Weight*2.20462))
		} else {
			parts = append(parts, fmt.Sprintf("weight %.1f kg", a.Weight))
		}
		if a.FTP > 0 {
			parts = append(parts, fmt.Sprintf("%.2f W/kg", a.FTP/a.Weight))
		}
	}
	b.WriteString(strings.Join(parts, " · ") + "\n")
	gearLine := func(kind string, list []summaryGear) {
		if len(list) == 0 {
			return
		}
		items := make([]string, 0, len(list))
		for _, g := range list {
			s := fmt.Sprintf("%s (id %s) %s", g.Name, g.ID, u.dist(g.Distance))
			if g.Primary {
				s += " primary"
			}
			if g.Retired {
				s += " retired"
			}
			items = append(items, s)
		}
		fmt.Fprintf(&b, "%s: %s\n", kind, strings.Join(items, " · "))
	}
	gearLine("bikes", a.Bikes)
	gearLine("shoes", a.Shoes)
	switch {
	case zerr != nil:
		fmt.Fprintf(&b, "zones: unavailable (%s)\n", failure(zerr).Text)
	default:
		if z.HeartRate != nil && len(z.HeartRate.Zones) > 0 {
			kind := "Strava's defaults"
			if z.HeartRate.CustomZones {
				kind = "custom"
			}
			fmt.Fprintf(&b, "heart-rate zones (%s): %s\n", kind, zoneList(z.HeartRate.Zones, " bpm"))
		}
		if z.Power != nil && len(z.Power.Zones) > 0 {
			fmt.Fprintf(&b, "power zones: %s\n", zoneList(z.Power.Zones, " W"))
		} else if a.FTP > 0 {
			b.WriteString("power zones: none from Strava; analyses use Coggan's from FTP\n")
		}
	}
	return mcpserver.Result{Text: b.String(), Sources: []mcpserver.Source{{
		URL: fmt.Sprintf("https://www.strava.com/athletes/%d", a.ID), Title: a.name(), Label: label, Snippet: "Strava profile"}}}
}

func zoneList(zs []zoneRange, unit string) string {
	parts := make([]string, 0, len(zs))
	for i, z := range zs {
		if z.Max < 0 || (i == len(zs)-1 && z.Max <= z.Min) {
			parts = append(parts, fmt.Sprintf("Z%d %.0f+%s", i+1, z.Min, unit))
		} else {
			parts = append(parts, fmt.Sprintf("Z%d %.0f–%.0f%s", i+1, z.Min, z.Max, unit))
		}
	}
	return strings.Join(parts, " · ")
}

func (t *tools) totals(ctx context.Context, _ json.RawMessage) mcpserver.Result {
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	st, err := t.svc.stats(ctx, user, a.ID)
	if err != nil {
		return failure(err)
	}
	u := unitsOf(a)
	var b strings.Builder
	b.WriteString("Strava totals (Strava counts only activities visible to Everyone; exact sums over every activity come from list_activities):\n")
	row := func(name string, x totals) {
		if x.Count == 0 {
			fmt.Fprintf(&b, "  %s: none\n", name)
			return
		}
		fmt.Fprintf(&b, "  %s: %d · %s · moving %s · %s\n", name, x.Count, u.dist(x.Distance), clock(x.MovingTime), u.elev(x.ElevationGain))
	}
	for _, p := range []struct {
		name            string
		ride, run, swim totals
	}{
		{"last 4 weeks", st.RecentRide, st.RecentRun, st.RecentSwim},
		{"this year", st.YTDRide, st.YTDRun, st.YTDSwim},
		{"all time", st.AllRide, st.AllRun, st.AllSwim},
	} {
		b.WriteString(p.name + ":\n")
		row("ride", p.ride)
		row("run", p.run)
		row("swim", p.swim)
	}
	if st.BiggestRideDistance > 0 {
		fmt.Fprintf(&b, "longest ride: %s\n", u.dist(st.BiggestRideDistance))
	}
	if st.BiggestClimbElevationGain > 0 {
		fmt.Fprintf(&b, "biggest climb: %s\n", u.elev(st.BiggestClimbElevationGain))
	}
	return mcpserver.Result{Text: b.String()}
}

func (t *tools) getActivity(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args struct {
		ID json.RawMessage `json:"id"`
	}
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("get_activity needs an activity id")
	}
	id, ok := parseID(args.ID)
	if !ok {
		return mcpserver.ErrorResult("get_activity needs an activity id")
	}
	user, a, fail := t.begin(ctx)
	if fail != nil {
		return *fail
	}
	d, res, ok := t.ownActivity(ctx, user, a, id)
	if !ok {
		return res
	}
	return mcpserver.Result{Text: describeActivity(d, unitsOf(a)), Sources: []mcpserver.Source{activitySource(d.summaryActivity)}}
}

// ownActivity reads an activity and makes sure it is the user's own: the
// tools never show anyone else's.
func (t *tools) ownActivity(ctx context.Context, user string, a athlete, id int64) (detailedActivity, mcpserver.Result, bool) {
	d, err := t.svc.activity(ctx, user, id)
	if err != nil {
		if notFound(err) || isStatus(err, 403) {
			return d, mcpserver.ErrorResult(fmt.Sprintf("Activity %d is not one of the user's activities on Strava.", id)), false
		}
		return d, failure(err), false
	}
	if d.Athlete.ID != 0 && a.ID != 0 && d.Athlete.ID != a.ID {
		return d, mcpserver.ErrorResult(fmt.Sprintf("Activity %d is not one of the user's activities on Strava.", id)), false
	}
	return d, mcpserver.Result{}, true
}

const (
	maxLaps     = 40
	maxSplits   = 100
	maxSegments = 60
)

func describeActivity(d detailedActivity, u units) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[1] Activity %d · %s · %s · %s", d.ID, d.Name, d.SportType, day(d.localStart()))
	if loc := strings.Trim(d.LocationCity+", "+d.LocationCountry, ", "); loc != "" {
		b.WriteString(" · " + loc)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "distance %s · moving %s · elapsed %s · elevation %s", u.dist(d.Distance), clock(float64(d.MovingTime)),
		clock(float64(d.ElapsedTime)), u.elev(d.TotalElevationGain))
	if d.ElevHigh != 0 || d.ElevLow != 0 {
		fmt.Fprintf(&b, " (%s–%s)", u.elev(d.ElevLow), u.elev(d.ElevHigh))
	}
	fmt.Fprintf(&b, " · avg %s", u.tempo(d.AverageSpeed, d.SportType))
	if d.MaxSpeed > 0 {
		b.WriteString(" · max " + u.speed(d.MaxSpeed))
	}
	b.WriteByte('\n')
	var parts []string
	if d.HasHeartrate {
		parts = append(parts, fmt.Sprintf("HR avg %.0f · max %.0f bpm", d.AverageHeartrate, d.MaxHeartrate))
	}
	if d.AverageWatts > 0 {
		p := fmt.Sprintf("power avg %.0f W", d.AverageWatts)
		if d.WeightedAverageWatts > 0 {
			p += fmt.Sprintf(" · weighted %.0f W", d.WeightedAverageWatts)
		}
		if d.MaxWatts > 0 {
			p += fmt.Sprintf(" · max %.0f W", d.MaxWatts)
		}
		if !d.DeviceWatts {
			p += " (estimated by Strava)"
		}
		parts = append(parts, p)
	}
	if d.Kilojoules > 0 {
		parts = append(parts, thousands(d.Kilojoules)+" kJ")
	}
	if d.Calories > 0 {
		parts = append(parts, thousands(d.Calories)+" kcal")
	}
	if d.AverageCadence > 0 {
		parts = append(parts, fmt.Sprintf("cadence %.0f", d.AverageCadence))
	}
	if d.AverageTemp != 0 {
		parts = append(parts, fmt.Sprintf("%.0f °C", d.AverageTemp))
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, " · ") + "\n")
	}
	parts = parts[:0]
	if d.SufferScore > 0 {
		parts = append(parts, fmt.Sprintf("relative effort %.0f", d.SufferScore))
	}
	if d.PerceivedExertion > 0 {
		parts = append(parts, fmt.Sprintf("perceived exertion %.0f/10", d.PerceivedExertion))
	}
	if d.WorkoutType != nil {
		if wt := workoutType(*d.WorkoutType); wt != "" {
			parts = append(parts, "workout "+wt)
		}
	}
	if d.DeviceName != "" {
		parts = append(parts, "device "+d.DeviceName)
	}
	if d.Gear != nil && d.Gear.Name != "" {
		parts = append(parts, "gear "+d.Gear.Name)
	}
	for _, f := range []struct {
		on   bool
		name string
	}{{d.Trainer, "indoor trainer"}, {d.Commute, "commute"}, {d.Manual, "manual entry"}, {d.Private, "private (Only Me)"}} {
		if f.on {
			parts = append(parts, f.name)
		}
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, " · ") + "\n")
	}
	fmt.Fprintf(&b, "%s · %s · %s · %s · %s\n", plural(d.KudosCount, "kudo", "kudos"), plural(d.CommentCount, "comment", "comments"),
		plural(d.Photos.Count, "photo", "photos"), plural(d.PRCount, "PR", "PRs"), plural(d.AchievementCount, "achievement", "achievements"))
	if s := oneLine(d.Description, 600); s != "" {
		b.WriteString("description: " + s + "\n")
	}
	if s := oneLine(d.PrivateNote, 600); s != "" {
		b.WriteString("private note: " + s + "\n")
	}
	if len(d.Laps) > 1 {
		fmt.Fprintf(&b, "\nlaps (%d):\n", len(d.Laps))
		for i, l := range d.Laps {
			if i == maxLaps {
				fmt.Fprintf(&b, "  … %d more laps\n", len(d.Laps)-maxLaps)
				break
			}
			fmt.Fprintf(&b, "  %d. %s · %s · %s", i+1, u.dist(l.Distance), clock(float64(l.MovingTime)), u.tempo(l.AverageSpeed, d.SportType))
			if l.AverageWatts > 0 {
				fmt.Fprintf(&b, " · %.0f W", l.AverageWatts)
			}
			if l.AverageHeartrate > 0 {
				fmt.Fprintf(&b, " · HR %.0f/%.0f", l.AverageHeartrate, l.MaxHeartrate)
			}
			if l.TotalElevationGain > 0 {
				b.WriteString(" · " + u.elev(l.TotalElevationGain))
			}
			b.WriteByte('\n')
		}
	}
	splits := d.SplitsMetric
	if u.imperial && len(d.SplitsStandard) > 0 {
		splits = d.SplitsStandard
	}
	if len(splits) > 0 {
		fmt.Fprintf(&b, "\nsplits per %s (%d): ", u.splitName(), len(splits))
		items := make([]string, 0, len(splits))
		for i, s := range splits {
			if i == maxSplits {
				items = append(items, fmt.Sprintf("… %d more", len(splits)-maxSplits))
				break
			}
			item := fmt.Sprintf("%d %s", s.Split, u.tempo(s.AverageSpeed, d.SportType))
			if s.AverageHeartrate > 0 {
				item += fmt.Sprintf(" HR %.0f", s.AverageHeartrate)
			}
			if math.Abs(s.ElevationDifference) >= 1 {
				item += " " + signed(s.ElevationDifference, u)
			}
			items = append(items, item)
		}
		b.WriteString(strings.Join(items, " · ") + "\n")
	}
	if len(d.BestEfforts) > 0 {
		items := make([]string, 0, len(d.BestEfforts))
		for _, e := range d.BestEfforts {
			item := e.Name + " " + clock(float64(e.ElapsedTime))
			if e.PRRank > 0 {
				item += fmt.Sprintf(" (PR %s)", ordinal(e.PRRank))
			}
			items = append(items, item)
		}
		b.WriteString("\nbest efforts: " + strings.Join(items, " · ") + "\n")
	}
	if len(d.SegmentEfforts) > 0 {
		fmt.Fprintf(&b, "\nsegment efforts (%d):\n", len(d.SegmentEfforts))
		for i, e := range d.SegmentEfforts {
			if i == maxSegments {
				fmt.Fprintf(&b, "  … %d more\n", len(d.SegmentEfforts)-maxSegments)
				break
			}
			fmt.Fprintf(&b, "  %s · %s · %s", e.Name, u.dist(e.Distance), clock(float64(e.ElapsedTime)))
			if e.Segment.AverageGrade != 0 {
				fmt.Fprintf(&b, " · %.1f %%", e.Segment.AverageGrade)
			}
			if e.AverageWatts > 0 {
				fmt.Fprintf(&b, " · %.0f W", e.AverageWatts)
			}
			if e.AverageHeartrate > 0 {
				fmt.Fprintf(&b, " · HR %.0f", e.AverageHeartrate)
			}
			if e.PRRank > 0 {
				fmt.Fprintf(&b, " · PR %s", ordinal(e.PRRank))
			}
			if e.KOMRank > 0 {
				fmt.Fprintf(&b, " · KOM rank %d", e.KOMRank)
			}
			fmt.Fprintf(&b, " · segment %d\n", e.Segment.ID)
		}
	}
	return b.String()
}

func signed(m float64, u units) string {
	if m > 0 {
		return "+" + u.elev(m)
	}
	return u.elev(m)
}

// workoutType names Strava's workout_type codes.
func workoutType(t int) string {
	switch t {
	case 1:
		return "race"
	case 2:
		return "long run"
	case 3:
		return "workout"
	case 11:
		return "race"
	case 12:
		return "workout"
	}
	return ""
}
