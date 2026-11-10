package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/sun"
	"github.com/trick77/loom/internal/weather"
	"github.com/trick77/ship/kit/httpx"
)

const (
	// forecastTimeout bounds the start page's forecast: the whole answer,
	// the unread runs included, waits on it. The sidecar answers from its
	// cache within the hour, so only its first call per place and hour waits.
	forecastTimeout = 4 * time.Second
	// todayRuns is how many unread run results the start page lists.
	todayRuns = 5
	// warningHours is how far ahead a warning looks today.
	warningHours = 12
	// warningRainChance is the chance of precipitation from which an hour
	// with rain, snow or a storm is a warning.
	warningRainChance = 50
	// eveningHour is when the page turns to tomorrow, local to the place (to
	// the browser while no place is known): in the evening the day ahead is
	// what is worth knowing.
	eveningHour = 20
	// tomorrowFrom and tomorrowTo bound tomorrow's hours shown and warned of.
	tomorrowFrom, tomorrowTo = 6, 22
	// rainHours is how many hours from now the rain card shows today.
	rainHours = 12
	// outlookDays is how many days after the shown one the outlook lists.
	outlookDays = 5
	// forecastDays is what the page asks the forecast for: today, tomorrow
	// and the outlook after tomorrow, which in the evening is shown.
	forecastDays = outlookDays + 2
	// airNowHours is how far ahead "now" may take the air index from when
	// the current hour has none.
	airNowHours = 3
	// airCurveHours is how many hours from now the air's curve shows today.
	airCurveHours = 24
)

// The day the page shows.
const (
	dayToday    = "today"
	dayTomorrow = "tomorrow"
)

type todayResponse struct {
	// Date and Day are the day shown: today, or tomorrow from eveningHour.
	Date        string          `json:"date"`
	Day         string          `json:"day"`
	Location    *location.Place `json:"location"`
	AskLocation bool            `json:"askLocation"`
	Sun         *todaySun       `json:"sun"`
	Weather     *todayWeather   `json:"weather"`
	Air         *todayAir       `json:"air"`
	Warning     *todayWarning   `json:"warning"`
	// Tonight is what comes before midnight; only while the page shows
	// tomorrow.
	Tonight *todayWarning `json:"tonight"`
	Runs    []todayRun    `json:"runs"`
	// RefreshAt is when the day shown changes next: the page reloads then.
	RefreshAt *time.Time `json:"refreshAt"`
}

type todaySun struct {
	Rise       *string   `json:"rise"`
	Set        *string   `json:"set"`
	DayMinutes int       `json:"dayMinutes"`
	Polar      sun.Polar `json:"polar"`
	// Progress is where the sun is between rise (0) and set (1) now; nil
	// for tomorrow.
	Progress *float64 `json:"progress"`
}

type todayWeather struct {
	// Now is the weather this hour; nil when the page shows tomorrow.
	Now *todayNow `json:"now"`
	// Day is the shown day's high, low and weather.
	Day   todayDay    `json:"day"`
	Hours []todayHour `json:"hours"`
	// Details, Rain and Outlook are the tile's further cards, each nil when
	// the forecast has no values for it.
	Details *todayDetails  `json:"details"`
	Rain    []todayRain    `json:"rain"`
	Outlook []todayOutlook `json:"outlook"`
}

// todayDetails is the shown day in numbers: today the air and wind now,
// tomorrow the wind at its strongest; both the day's UV, rain and sun.
type todayDetails struct {
	FeelsLike        *float64 `json:"feelsLike"`
	Humidity         *int     `json:"humidity"`
	Wind             *float64 `json:"wind"`
	Gusts            *float64 `json:"gusts"`
	WindDirection    *int     `json:"windDirection"`
	UVMax            *float64 `json:"uvMax"`
	PrecipitationSum *float64 `json:"precipitationSum"`
	SunshineHours    *float64 `json:"sunshineHours"`
}

type todayRain struct {
	Time          string   `json:"time"`
	Precipitation *float64 `json:"precipitation"`
	Probability   *int     `json:"probability"`
}

type todayOutlook struct {
	Date                     string  `json:"date"`
	High                     float64 `json:"high"`
	Low                      float64 `json:"low"`
	Code                     int     `json:"code"`
	PrecipitationProbability *int    `json:"precipitationProbability"`
}

type todayNow struct {
	Temperature float64 `json:"temperature"`
	Code        int     `json:"code"`
}

type todayDay struct {
	High                     float64 `json:"high"`
	Low                      float64 `json:"low"`
	Code                     int     `json:"code"`
	PrecipitationProbability *int    `json:"precipitationProbability"`
}

type todayHour struct {
	Time                     string  `json:"time"`
	Temperature              float64 `json:"temperature"`
	Code                     int     `json:"code"`
	PrecipitationProbability *int    `json:"precipitationProbability"`
}

// todayAir is the air on the shown day: today the index now, tomorrow its
// highest (Max), and the pollen worth a load class.
type todayAir struct {
	AQI   *int   `json:"aqi"`
	Level string `json:"level"`
	Max   bool   `json:"max"`
	// Pollen is nil where the forecast has none at all (outside Europe, out
	// of season), empty where it has some but none worth a class: "hardly
	// any" must not stand for "unknown".
	Pollen []todayPollen `json:"pollen"`
	// Hours is the index hour by hour for the curve: today from now, tomorrow
	// its shown hours as far as the air reaches; an hour without one is nil,
	// a gap in the curve.
	Hours []todayAirHour `json:"hours"`
}

type todayAirHour struct {
	Time string `json:"time"`
	AQI  *int   `json:"aqi"`
}

type todayPollen struct {
	Kind  string `json:"kind"`
	Level string `json:"level"`
}

type todayWarning struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	// Day is the day From falls on: today's 12 h may reach into tomorrow.
	Day string `json:"day"`
	// Chance is the precipitation chance at From; nil for frost.
	Chance *int `json:"chance"`
}

type todayRun struct {
	ID        string    `json:"id"`
	ThreadID  *string   `json:"threadId"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// handleToday is the start page's day: the user's place, its weather and sun,
// and the schedule results not read yet. From eveningHour on it is tomorrow's
// weather and sun. Every part is optional: one that fails is left out, never
// the page.
func (s *server) handleToday(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	user.Timezone = s.rememberTimezone(ctx, user, r)
	now := s.clock()
	out := todayResponse{Runs: s.todayRuns(ctx, user)}
	if s.locationOn() {
		// The facts may have changed since the last sweep: derive in the
		// background, the next visit shows it. Unchanged facts cost two queries.
		s.background.Spawn(context.WithoutCancel(ctx), "user_location", func(ctx context.Context) {
			if err := s.resolveLocation(ctx, user); err != nil {
				slog.Warn("derive location failed", "user_id", user.ID, "err", err)
			}
		})
		place, err := s.userPlace(ctx, user)
		if err != nil {
			slog.Warn("read location failed", "user_id", user.ID, "err", err)
		}
		// Ask only once the facts have been read and name no place: while a
		// derivation is pending the memory may already know it, and an answer
		// would add a second home fact.
		out.Location, out.AskLocation = place, err == nil && place == nil && s.locationSettled(ctx, user)
	}
	// One clock for the day shown, its evening, the sun and the weather: the
	// place's when a place is known (its evening is the user's, they are
	// there), else the browser's. refreshAt tells the page when it turns.
	loc := userZone(user)
	if out.Location != nil {
		loc = weather.Zone(out.Location.Timezone, user.Timezone)
	}
	local := now.In(loc)
	tomorrow := local.Hour() >= eveningHour
	shown := local
	out.Day = dayToday
	if tomorrow {
		shown, out.Day = local.AddDate(0, 0, 1), dayTomorrow
	}
	out.Date = shown.Format(time.DateOnly)
	refreshAt := nextTurn(local)
	out.RefreshAt = &refreshAt
	if p := out.Location; p != nil {
		out.Sun = sunOn(now, shown, p, loc)
		// The air shows on the weather's tile only: without the weather it is
		// left out.
		weatherCtx, cancel := context.WithTimeout(ctx, forecastTimeout)
		report, err := weather.GetWeather(weatherCtx, s.mcp, p.Latitude, p.Longitude, loc.String(), forecastDays)
		cancel()
		switch f := report.Forecast; {
		case err != nil || len(f.Daily) == 0:
			slog.Warn("forecast failed", "err", err, "days", len(f.Daily))
		case tomorrow:
			out.Weather, out.Warning, out.Tonight = tomorrowForecast(f, local, shown)
		default:
			out.Weather, out.Warning = todayForecast(f, local)
		}
		if out.Weather != nil && report.Air != nil {
			out.Air = airOn(*report.Air, local, shown, tomorrow)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// nextTurn is when the day shown next changes after local: at eveningHour
// to tomorrow, at midnight the shown tomorrow becomes today.
func nextTurn(local time.Time) time.Time {
	y, m, d := local.Date()
	evening := time.Date(y, m, d, eveningHour, 0, 0, 0, local.Location())
	if local.Before(evening) {
		return evening
	}
	return time.Date(y, m, d+1, 0, 0, 0, 0, local.Location())
}

// locationSettled reports whether the facts as they are now were read for a
// place: derived, or the derivation failed (asking beats waiting on a model
// that keeps failing), or there are no facts to read. An error counts as not
// settled (no question asked).
func (s *server) locationSettled(ctx context.Context, user auth.User) bool {
	if s.thread == nil {
		return false
	}
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return false
	}
	if len(facts) == 0 {
		return true
	}
	hash := location.HashFacts(facts)
	if failed, ok := s.locationFailed.Load(user.ID); ok && failed == hash {
		return true
	}
	kept, err := s.locations.FactsHash(ctx, user.ID)
	return err == nil && kept == hash
}

// todayRuns is the schedule results the user has not read, newest first.
func (s *server) todayRuns(ctx context.Context, user auth.User) []todayRun {
	runs := []todayRun{}
	if s.schedules == nil {
		return runs
	}
	notes, err := s.schedules.ListUnread(ctx, user.ID, schedule.KindScheduleRun, todayRuns)
	if err != nil {
		slog.Warn("list unread runs failed", "user_id", user.ID, "err", err)
		return runs
	}
	for _, n := range notes {
		runs = append(runs, todayRun{ID: n.ID, ThreadID: n.ThreadID, Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt})
	}
	return runs
}

// sunOn is the sun on the shown day (a time on it, in loc); where the sun is
// now only when the shown day is today.
func sunOn(now, shown time.Time, p *location.Place, loc *time.Location) *todaySun {
	day := sun.On(shown, p.Latitude, p.Longitude, loc)
	out := &todaySun{
		DayMinutes: int(day.Length().Round(time.Minute) / time.Minute),
		Polar:      day.Polar,
	}
	if day.Polar != sun.NotPolar {
		return out
	}
	rise, set := day.Rise.Format("15:04"), day.Set.Format("15:04")
	out.Rise, out.Set = &rise, &set
	if length := day.Length(); length > 0 && sameDay(now.In(loc), shown.In(loc)) {
		progress := math.Round(float64(now.Sub(day.Rise))/float64(length)*1000) / 1000
		out.Progress = &progress
	}
	return out
}

func sameDay(a, b time.Time) bool {
	return a.Format(time.DateOnly) == b.Format(time.DateOnly)
}

// airOn is the air on the shown day: today the index this hour, tomorrow its
// highest; nil when neither the index nor any pollen is known.
func airOn(a weather.Air, local, shown time.Time, tomorrow bool) *todayAir {
	date := shown.Format(time.DateOnly)
	out := &todayAir{Max: tomorrow, Hours: airHoursOn(a, local, date, tomorrow)}
	var aqi *float64
	if !tomorrow {
		// The first hour from now with an index, within airNowHours: an
		// hour the provider has none for is skipped, a later one is not now.
		current, seen := hourStamp(local), 0
		for _, h := range a.Hourly {
			if h.Time < current {
				continue
			}
			if seen++; seen > airNowHours {
				break
			}
			if h.AQI != nil {
				aqi = h.AQI
				break
			}
		}
	}
	for _, d := range a.Daily {
		if d.Date != date {
			continue
		}
		if tomorrow {
			aqi = d.AQIMax
		}
		for _, kind := range weather.PollenOrder {
			v, ok := d.Pollen[kind]
			if !ok {
				continue
			}
			if out.Pollen == nil {
				out.Pollen = []todayPollen{}
			}
			if level := weather.PollenLevel(kind, v); level != "" {
				out.Pollen = append(out.Pollen, todayPollen{Kind: kind, Level: level})
			}
		}
	}
	if aqi != nil {
		v := int(math.Round(*aqi))
		out.AQI, out.Level = &v, weather.AQILevel(v)
	}
	if out.AQI == nil && len(out.Pollen) == 0 {
		return nil
	}
	return out
}

// airHoursOn is the index hour by hour: today airCurveHours from now,
// tomorrow from tomorrowFrom to tomorrowTo as far as the answer reaches (the
// air tool lists 24 hours from now: from 20:00 tomorrow's end is cut).
func airHoursOn(a weather.Air, local time.Time, date string, tomorrow bool) []todayAirHour {
	out := []todayAirHour{}
	current := hourStamp(local)
	for _, h := range a.Hourly {
		if len(h.Time) < 13 {
			continue
		}
		if tomorrow {
			hour, err := strconv.Atoi(h.Time[11:13])
			if h.Time[:10] != date || err != nil || hour < tomorrowFrom || hour > tomorrowTo {
				continue
			}
		} else if h.Time < current {
			continue
		} else if len(out) == airCurveHours {
			break
		}
		hour := todayAirHour{Time: clock(h.Time)}
		if h.AQI != nil {
			v := int(math.Round(*h.AQI))
			hour.AQI = &v
		}
		out = append(out, hour)
	}
	return out
}

// hourStamp is local's hour as the forecasts write their times.
func hourStamp(local time.Time) string {
	return local.Truncate(time.Hour).Format("2006-01-02T15:04")
}

// todayForecast reads the start page's weather off the forecast from the
// current hour on: "now" between it and the next, five hours three apart,
// today's high and low, and a warning for the next hours. The contract does
// not promise the hours start now, so the current one is looked up; a
// forecast with no hour from now on is none.
func todayForecast(f weather.Forecast, now time.Time) (*todayWeather, *todayWarning) {
	current := hourStamp(now)
	start := currentHour(f, now)
	if start < 0 {
		return nil, nil
	}
	f.Hourly = f.Hourly[start:]
	first := f.Hourly[0]
	temp := first.Temperature
	if len(f.Hourly) > 1 && first.Time == current {
		frac := float64(now.Minute()) / 60
		temp += (f.Hourly[1].Temperature - temp) * frac
	}
	day, ok := dayOf(f, now)
	ownDay := day
	if !ok {
		// A daily list that does not name today: its first day stands in for
		// the range, never for the details (its UV, rain and sun are another
		// day's).
		day, _ = dayOf(f, time.Time{})
		ownDay = weather.Day{}
	}
	out := &todayWeather{
		Now:     &todayNow{Temperature: math.Round(temp*10) / 10, Code: first.Code},
		Day:     dayView(day),
		Hours:   []todayHour{},
		Details: detailsOf(&first, ownDay),
		Rain:    rainIn(f.Hourly[:min(rainHours, len(f.Hourly))]),
		Outlook: outlookAfter(f, now),
	}
	for i := 1; i < len(f.Hourly) && len(out.Hours) < 5; i += 3 {
		out.Hours = append(out.Hours, hourOf(f, i))
	}
	return out, warningIn(f, 0, warningHours, now)
}

// tomorrowForecast reads tomorrow off the forecast: its high and low, five
// hours from tomorrowFrom three apart, a warning for tomorrow's daytime and
// one for tonight, from now (local) until midnight. Apart: the
// page shows tonight on its own, under tomorrow's heading it read as
// tomorrow's. A forecast that does not reach tomorrow, or whose days do not
// name it, is none: never today's numbers under tomorrow's name.
func tomorrowForecast(f weather.Forecast, local, tomorrow time.Time) (weather *todayWeather, warning, tonight *todayWarning) {
	day, ok := dayOf(f, tomorrow)
	if !ok {
		return nil, nil, nil
	}
	date := tomorrow.Format(time.DateOnly)
	from, to, midnight := -1, -1, -1
	for i, h := range f.Hourly {
		if len(h.Time) < 13 || h.Time[:10] != date {
			continue
		}
		if midnight < 0 {
			midnight = i
		}
		hour, err := strconv.Atoi(h.Time[11:13])
		if err != nil {
			continue
		}
		if from < 0 && hour >= tomorrowFrom {
			from = i
		}
		if hour <= tomorrowTo {
			to = i
		}
	}
	if from < 0 || to < from {
		return nil, nil, nil
	}
	out := &todayWeather{
		Day:     dayView(day),
		Hours:   []todayHour{},
		Details: detailsOf(nil, day),
		Rain:    rainIn(f.Hourly[from : to+1]),
		Outlook: outlookAfter(f, tomorrow),
	}
	for i := from; i <= to && len(out.Hours) < 5; i += 3 {
		out.Hours = append(out.Hours, hourOf(f, i))
	}
	// Tonight ends at midnight: what comes in the small hours falls while
	// everyone sleeps, and tomorrow's warning starts at tomorrowFrom.
	if now := currentHour(f, local); now >= 0 && now < midnight {
		tonight = warningIn(f, now, midnight-1-now, local)
	}
	return out, warningIn(f, from, to-from, local), tonight
}

// currentHour is the index of the forecast's hour that holds local, or the
// first after it; -1 when the forecast ended before.
func currentHour(f weather.Forecast, local time.Time) int {
	current := hourStamp(local)
	for i, h := range f.Hourly {
		if h.Time >= current {
			return i
		}
	}
	return -1
}

// dayOf is the forecast's day on date; ok is false when the forecast does
// not name it, the first day returned then. A zero date asks for the first.
func dayOf(f weather.Forecast, date time.Time) (weather.Day, bool) {
	day, ok := f.Daily[0], date.IsZero()
	for _, d := range f.Daily {
		if !date.IsZero() && d.Date == date.Format(time.DateOnly) {
			day, ok = d, true
			break
		}
	}
	return day, ok
}

func dayView(d weather.Day) todayDay {
	return todayDay{High: d.High, Low: d.Low, Code: d.Code, PrecipitationProbability: d.PrecipitationProbability}
}

// detailsOf is the details card: with now (today) the air and wind this
// hour, without (tomorrow) the day's strongest wind; nil when the forecast
// carries none of it.
func detailsOf(now *weather.Hour, day weather.Day) *todayDetails {
	d := todayDetails{UVMax: day.UVIndexMax, PrecipitationSum: day.PrecipitationSum, SunshineHours: day.SunshineHours,
		Wind: day.WindSpeedMax, Gusts: day.WindGustsMax, WindDirection: day.WindDirection}
	if now != nil {
		d.FeelsLike, d.Humidity = now.ApparentTemperature, now.Humidity
		d.Wind, d.Gusts, d.WindDirection = now.WindSpeed, now.WindGusts, now.WindDirection
	}
	if d == (todayDetails{}) {
		return nil
	}
	return &d
}

// rainIn is the rain card's hours; nil when none says how much or how likely.
func rainIn(hours []weather.Hour) []todayRain {
	var out []todayRain
	known := false
	for _, h := range hours {
		known = known || h.Precipitation != nil || h.PrecipitationProbability != nil
		out = append(out, todayRain{Time: clock(h.Time), Precipitation: h.Precipitation, Probability: h.PrecipitationProbability})
	}
	if !known {
		return nil
	}
	return out
}

// outlookAfter is up to outlookDays days after shown; nil when the forecast
// ends with it.
func outlookAfter(f weather.Forecast, shown time.Time) []todayOutlook {
	date := shown.Format(time.DateOnly)
	var out []todayOutlook
	for _, d := range f.Daily {
		if d.Date > date && len(out) < outlookDays {
			out = append(out, todayOutlook{Date: d.Date, High: d.High, Low: d.Low, Code: d.Code,
				PrecipitationProbability: d.PrecipitationProbability})
		}
	}
	return out
}

func hourOf(f weather.Forecast, i int) todayHour {
	h := f.Hourly[i]
	return todayHour{Time: clock(h.Time), Temperature: h.Temperature, Code: h.Code, PrecipitationProbability: h.PrecipitationProbability}
}

// warningIn is the first warning in the hours from start through start+span:
// precipitation likely enough, else frost; its Day says whether it falls on
// local's day or the next.
func warningIn(f weather.Forecast, start, span int, local time.Time) *todayWarning {
	today := local.Format(time.DateOnly)
	at := func(kind, t string) *todayWarning {
		day := dayTomorrow
		if len(t) >= 10 && t[:10] == today {
			day = dayToday
		}
		return &todayWarning{Kind: kind, From: clock(t), Day: day}
	}
	var warning *todayWarning
	for i := start; i < len(f.Hourly) && i <= start+span; i++ {
		h := f.Hourly[i]
		if kind := precipitation(h.Code); kind != "" && h.PrecipitationProbability != nil && *h.PrecipitationProbability >= warningRainChance {
			w := at(kind, h.Time)
			w.Chance = h.PrecipitationProbability
			return w
		}
		if warning == nil && h.Temperature <= 0 {
			warning = at("frost", h.Time)
		}
	}
	return warning
}

// precipitation names what falls for a WMO weather code; "" when nothing.
func precipitation(code int) string {
	switch {
	case code >= 95:
		return "storm"
	case code >= 71 && code <= 77, code == 85, code == 86:
		return "snow"
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return "rain"
	}
	return ""
}

// clock is a forecast time's "15:04".
func clock(local string) string {
	if t, err := time.Parse("2006-01-02T15:04", local); err == nil {
		return t.Format("15:04")
	}
	return local
}
