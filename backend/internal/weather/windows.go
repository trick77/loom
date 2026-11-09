package weather

import (
	"math"
	"slices"
	"time"

	"github.com/trick77/loom/internal/sun"
)

// Activity is what the user goes out for; it sets the default length and how
// much wind and cold weigh.
type Activity string

// The activities: a run (a jog, a walk) and a bike ride.
const (
	Run  Activity = "run"
	Ride Activity = "ride"
)

// DefaultMinutes is the activity's usual length: a run an hour, a ride two.
func (a Activity) DefaultMinutes() int {
	if a == Ride {
		return 120
	}
	return 60
}

// comfort is the feels-like band an activity is pleasant in and the wind it
// takes. A ride is colder (its own wind) and catches far more of the wind.
type comfort struct {
	low, high  float64
	limits     Limits
	windWeight float64
}

func (a Activity) comfort() comfort {
	if a == Ride {
		return comfort{low: 15, high: 24, limits: a.Limits(), windWeight: 2}
	}
	return comfort{low: 12, high: 20, limits: a.Limits(), windWeight: 1}
}

// Wind levels: past Limits' first pair the wind is worth a word, past the
// second a warning.
const (
	WindWindy  = "windy"
	WindStrong = "strong"
)

// Limits are an activity's wind thresholds in km/h, as forecast (10 m above
// ground; at body height it is about a third less in the open). Sustained
// wind: a rider feels 10 km/h and works hard at 20 (felt), so 15 forecast is
// worth a word; a runner's cost grows with the square of the wind and
// barely shows below Beaufort 4 (20). Both find Beaufort 6 (39) the edge of
// bearable. Gusts are the danger on a bike (a side gust throws it across the
// lane): a word from 30, a warning from 45; a runner from 40 and 55, short
// of the 63 the DWD calls stormy.
type Limits struct {
	Wind        float64 `json:"wind"`
	WindStrong  float64 `json:"windStrong"`
	Gusts       float64 `json:"gusts"`
	GustsStrong float64 `json:"gustsStrong"`
}

// Limits are the activity's wind thresholds.
func (a Activity) Limits() Limits {
	if a == Ride {
		return Limits{Wind: 15, WindStrong: 39, Gusts: 30, GustsStrong: 45}
	}
	return Limits{Wind: 20, WindStrong: 39, Gusts: 40, GustsStrong: 55}
}

// Level is the wind level of a wind and its gusts (either may be unknown):
// WindStrong, WindWindy or "".
func (l Limits) Level(wind, gusts *float64) string {
	past := func(v *float64, at float64) bool { return v != nil && *v >= at }
	switch {
	case past(wind, l.WindStrong) || past(gusts, l.GustsStrong):
		return WindStrong
	case past(wind, l.Wind) || past(gusts, l.Gusts):
		return WindWindy
	}
	return ""
}

// Why a day has no window.
const (
	// NoneDark: polar night, the sun never rises.
	NoneDark = "dark"
	// NoneShort: the day's daylight is shorter than the activity.
	NoneShort = "short"
	// NoneOver: today's daylight left is shorter than the activity.
	NoneOver = "over"
	// NoneNoForecast: the forecast has no hours for the daylight.
	NoneNoForecast = "noForecast"
)

const (
	// wetChance and wetMM make an hour wet: rain likely, or enough of it to
	// soak a shirt. A WMO code of drizzle or worse is wet whatever the chance.
	wetChance = 50
	wetMM     = 0.2
	wetCode   = 51
	// sunnyMinutes in an hour of daylight is sun worth waiting for.
	sunnyMinutes = 20
	// step is the grid a window starts on, past the full hours.
	step = 15 * time.Minute
	// maxWindows is how many windows a dry day lists, maxWetWindows a wet one.
	maxWindows    = 3
	maxWetWindows = 2
	// noteAQI is the index from which the air is worth a word (moderate).
	noteAQI = 41
)

// WindowsInput is a day to go out on: its forecast (and, optional, air), the
// sun on it and the time now.
type WindowsInput struct {
	Forecast Forecast
	Air      *Air
	// Day is any time on the day asked, Now the time now; both read in Loc,
	// the place's zone.
	Day, Now time.Time
	Loc      *time.Location
	Sun      sun.Day
	Minutes  int
	Activity Activity
}

// Outlook is when to go out on a day. Times are the place's, "15:04".
type Outlook struct {
	Date string `json:"date"`
	// Now is set when the day is today.
	Now     string `json:"now,omitempty"`
	Sunrise string `json:"sunrise,omitempty"`
	Sunset  string `json:"sunset,omitempty"`
	Polar   string `json:"polar,omitempty"`
	// Dry is every daylight hour still to come dry; SunExpected is sun in at
	// least one of them.
	Dry         bool `json:"dry"`
	SunExpected bool `json:"sunExpected"`
	// Windows are best first: the dry ones only, when there are any.
	Windows []Window `json:"windows"`
	// None is why there is no window (NoneDark, NoneShort, NoneOver,
	// NoneNoForecast).
	None string   `json:"none,omitempty"`
	Air  *AirNote `json:"air,omitempty"`
}

// Window is a span to go out in, read off the hours it overlaps.
type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
	Dry  bool   `json:"dry"`
	// SunMinutes is the window's sunshine.
	SunMinutes int `json:"sunMinutes"`
	// RainChance is its highest chance of precipitation, Precipitation its
	// total in mm.
	RainChance    int     `json:"rainChance"`
	Precipitation float64 `json:"precipitation"`
	FeelsLikeMin  float64 `json:"feelsLikeMin"`
	FeelsLikeMax  float64 `json:"feelsLikeMax"`
	// WindMax is the window's strongest wind (km/h), WindFrom where that
	// blows from (8-point compass), GustsMax its strongest gust. Wind is
	// their level for the activity (WindWindy, WindStrong, "").
	WindMax  *float64 `json:"windMax,omitempty"`
	WindFrom string   `json:"windFrom,omitempty"`
	GustsMax *float64 `json:"gustsMax,omitempty"`
	Wind     string   `json:"wind,omitempty"`

	start time.Time
	// sun is the sunshine that draws: none from hours already too warm.
	sun  float64
	pain float64
}

// AirNote is the air worth a word in the best window: the index's band from
// moderate on, and the pollen kinds from a moderate load.
type AirNote struct {
	AQI    *int              `json:"aqi,omitempty"`
	Level  string            `json:"level,omitempty"`
	Pollen map[string]string `json:"pollen,omitempty"`
}

// Windows is when to go out on in.Day for in.Minutes: windows wholly in
// daylight (never before sunrise or past sunset, never begun), dry before
// wet, then sunnier, then nearer the activity's comfort, then earlier.
func Windows(in WindowsInput) Outlook {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	dayStart := midnight(in.Day.In(loc))
	now := in.Now.In(loc)
	out := Outlook{Date: dayStart.Format(time.DateOnly), Windows: []Window{}, Polar: string(in.Sun.Polar)}
	today := dayStart.Equal(midnight(now))
	if today {
		out.Now = now.Format("15:04")
	}
	dur := time.Duration(max(in.Minutes, 1)) * time.Minute
	var rise, set time.Time
	switch in.Sun.Polar {
	case sun.PolarNight:
		out.None = NoneDark
		return out
	case sun.PolarDay:
		rise, set = dayStart, dayStart.AddDate(0, 0, 1)
	default:
		rise, set = in.Sun.Rise.In(loc), in.Sun.Set.In(loc)
		out.Sunrise, out.Sunset = rise.Format("15:04"), set.Format("15:04")
	}
	// Starts sit on the step grid: a day is short when no start on it fits
	// between sunrise and sunset, over when none is left after now.
	latest := floor(set.Add(-dur))
	if latest.Before(ceil(rise)) {
		out.None = NoneShort
		return out
	}
	earliest := ceil(rise)
	if now.After(earliest) {
		earliest = ceil(now)
	}
	hours := hoursOf(in.Forecast, loc)
	if len(hours) == 0 {
		out.None = NoneNoForecast
		return out
	}
	out.Dry, out.SunExpected = dayAhead(hours, earliest, set)
	if latest.Before(earliest) {
		out.None = NoneOver
		return out
	}
	c := in.Activity.comfort()
	var candidates []Window
	for _, start := range starts(earliest, latest) {
		if w, ok := window(hours, start, dur, c); ok {
			candidates = append(candidates, w)
		}
	}
	if len(candidates) == 0 {
		out.None = NoneNoForecast
		return out
	}
	out.Windows = pick(candidates, dur)
	out.Air = airNote(in.Air, out.Windows[0], dur, out.Date)
	return out
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// hourStart is the start of t's local hour, right in zones off by a half hour.
func hourStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

// ceil and floor put a time on the step grid of its hour.
func ceil(t time.Time) time.Time {
	h := hourStart(t)
	n := (t.Sub(h) + step - 1) / step
	return h.Add(n * step)
}

func floor(t time.Time) time.Time {
	h := hourStart(t)
	return h.Add(t.Sub(h) / step * step)
}

// starts are the earliest start, every full hour after it and the latest.
func starts(earliest, latest time.Time) []time.Time {
	out := []time.Time{earliest}
	for h := hourStart(earliest).Add(time.Hour); !h.After(latest); h = h.Add(time.Hour) {
		out = append(out, h)
	}
	if !latest.Equal(out[len(out)-1]) {
		out = append(out, latest)
	}
	return out
}

func hoursOf(f Forecast, loc *time.Location) map[int64]Hour {
	out := map[int64]Hour{}
	for _, h := range f.Hourly {
		t, err := time.ParseInLocation("2006-01-02T15:04", h.Time, loc)
		if err == nil {
			out[t.Unix()] = h
		}
	}
	return out
}

func wet(h Hour) bool {
	return h.Code >= wetCode || (h.PrecipitationProbability != nil && *h.PrecipitationProbability >= wetChance) ||
		(h.Precipitation != nil && *h.Precipitation >= wetMM)
}

// sunshine is the hour's minutes of sun; without them (another server) the
// weather code stands in.
func sunshine(h Hour) float64 {
	if h.SunshineMinutes != nil {
		return float64(*h.SunshineMinutes)
	}
	switch h.Code {
	case 0:
		return 55
	case 1:
		return 45
	case 2:
		return 25
	}
	return 0
}

// dayAhead reads the daylight hours from from to to: all dry, any sunny.
func dayAhead(hours map[int64]Hour, from, to time.Time) (dry, sunny bool) {
	dry, seen := true, false
	for h := hourStart(from); h.Before(to); h = h.Add(time.Hour) {
		hour, ok := hours[h.Unix()]
		if !ok {
			continue
		}
		seen = true
		dry = dry && !wet(hour)
		sunny = sunny || sunshine(hour) >= sunnyMinutes
	}
	return dry && seen, sunny
}

// window reads the hours [start, start+dur) overlaps, each weighed by its
// share; ok is false when the forecast lacks one of them.
func window(hours map[int64]Hour, start time.Time, dur time.Duration, c comfort) (Window, bool) {
	end := start.Add(dur)
	w := Window{From: start.Format("15:04"), To: end.Format("15:04"), Dry: true, start: start,
		FeelsLikeMin: math.Inf(1), FeelsLikeMax: math.Inf(-1)}
	sunAll := 0.0
	for h := hourStart(start); h.Before(end); h = h.Add(time.Hour) {
		hour, ok := hours[h.Unix()]
		if !ok {
			return Window{}, false
		}
		from, to := h, h.Add(time.Hour)
		if start.After(from) {
			from = start
		}
		if end.Before(to) {
			to = end
		}
		share := float64(to.Sub(from)) / float64(time.Hour)
		w.Dry = w.Dry && !wet(hour)
		feels := hour.Temperature
		if hour.ApparentTemperature != nil {
			feels = *hour.ApparentTemperature
		}
		sunMinutes := sunshine(hour) * share
		sunAll += sunMinutes
		// Past the comfort band the sun is no draw: it only adds heat.
		if feels <= c.high {
			w.sun += sunMinutes
		}
		if hour.PrecipitationProbability != nil {
			w.RainChance = max(w.RainChance, *hour.PrecipitationProbability)
		}
		if hour.Precipitation != nil {
			w.Precipitation += *hour.Precipitation * share
		}
		w.FeelsLikeMin, w.FeelsLikeMax = min(w.FeelsLikeMin, feels), max(w.FeelsLikeMax, feels)
		pain := max(0, c.low-feels) + max(0, feels-c.high)
		if hour.WindSpeed != nil {
			v := *hour.WindSpeed
			if w.WindMax == nil || v > *w.WindMax {
				w.WindMax, w.WindFrom = &v, ""
				if hour.WindDirection != nil {
					w.WindFrom = compass(*hour.WindDirection)
				}
			}
			pain += max(0, v-c.limits.Wind) / 5 * c.windWeight
		}
		if hour.WindGusts != nil {
			g := *hour.WindGusts
			if w.GustsMax == nil || g > *w.GustsMax {
				w.GustsMax = &g
			}
			pain += max(0, g-c.limits.Gusts) / 5 * c.windWeight
		}
		w.pain += pain * share
	}
	w.Wind = c.limits.Level(w.WindMax, w.GustsMax)
	w.pain /= dur.Hours()
	w.SunMinutes = int(math.Round(sunAll))
	w.Precipitation = math.Round(w.Precipitation*10) / 10
	return w, true
}

// compass is where a wind of deg degrees blows from, on 8 points.
func compass(deg int) string {
	points := [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	return points[int(math.Round(float64(((deg%360)+360)%360)/45))%8]
}

// pick ranks the windows and keeps the best that do not overlap: the dry
// ones when there are any, else the driest.
func pick(all []Window, dur time.Duration) []Window {
	dry := slices.ContainsFunc(all, func(w Window) bool { return w.Dry })
	limit := maxWindows
	if dry {
		all = slices.DeleteFunc(all, func(w Window) bool { return !w.Dry })
	} else {
		limit = maxWetWindows
	}
	slices.SortStableFunc(all, func(a, b Window) int {
		// Strong wind anywhere in a window sinks it below every other: no
		// sun, and no less rain, makes up for a gust that throws a bike
		// across the lane.
		if sa, sb := a.Wind == WindStrong, b.Wind == WindStrong; sa != sb {
			if sa {
				return 1
			}
			return -1
		}
		if !dry {
			if a.Precipitation != b.Precipitation {
				return cmp(a.Precipitation, b.Precipitation)
			}
			if a.RainChance != b.RainChance {
				return a.RainChance - b.RainChance
			}
		}
		// Sun in quarters of the window: a few minutes more is no reason
		// to go colder.
		if sa, sb := sunQuarters(a, dur), sunQuarters(b, dur); sa != sb {
			return sb - sa
		}
		if pa, pb := math.Round(a.pain*2), math.Round(b.pain*2); pa != pb {
			return cmp(pa, pb)
		}
		if a.RainChance != b.RainChance {
			return a.RainChance - b.RainChance
		}
		return a.start.Compare(b.start)
	})
	var out []Window
	for _, w := range all {
		if len(out) == limit {
			break
		}
		if !slices.ContainsFunc(out, func(o Window) bool {
			return w.start.Before(o.start.Add(dur)) && o.start.Before(w.start.Add(dur))
		}) {
			out = append(out, w)
		}
	}
	return out
}

func sunQuarters(w Window, dur time.Duration) int {
	return int(math.Round(w.sun / dur.Minutes() * 4))
}

func cmp(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// airNote is the air worth a word in w on date; nil when none is.
func airNote(a *Air, w Window, dur time.Duration, date string) *AirNote {
	if a == nil {
		return nil
	}
	note := &AirNote{}
	from, to := hourStart(w.start), w.start.Add(dur)
	for _, h := range a.Hourly {
		t, err := time.ParseInLocation("2006-01-02T15:04", h.Time, w.start.Location())
		if err != nil || h.AQI == nil || t.Before(from) || !t.Before(to) {
			continue
		}
		if v := int(math.Round(*h.AQI)); v >= noteAQI && (note.AQI == nil || v > *note.AQI) {
			note.AQI, note.Level = &v, AQILevel(v)
		}
	}
	for _, d := range a.Daily {
		if d.Date != date {
			continue
		}
		for _, kind := range PollenOrder {
			if v, ok := d.Pollen[kind]; ok {
				if level := PollenLevel(kind, v); level != "" && level != "low" {
					if note.Pollen == nil {
						note.Pollen = map[string]string{}
					}
					note.Pollen[kind] = level
				}
			}
		}
	}
	if note.AQI == nil && note.Pollen == nil {
		return nil
	}
	return note
}
