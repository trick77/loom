package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/sun"
	"github.com/trick77/ship/kit/httpx"
)

// forecastTool is the weather sidecar's forecast (docs/weather.md).
var forecastTool = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherForecastTool)

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
	Warning     *todayWarning   `json:"warning"`
	Runs        []todayRun      `json:"runs"`
	// RefreshAt is when the day shown changes next: the page reloads then.
	RefreshAt *time.Time `json:"refreshAt"`
}

type todaySun struct {
	Rise         *string   `json:"rise"`
	Set          *string   `json:"set"`
	DayMinutes   int       `json:"dayMinutes"`
	DeltaMinutes int       `json:"deltaMinutes"`
	Polar        sun.Polar `json:"polar"`
	// Progress is where the sun is between rise (0) and set (1) now; nil
	// for tomorrow.
	Progress *float64 `json:"progress"`
}

type todayWeather struct {
	// Now is the weather this hour; nil when the page shows tomorrow.
	Now *todayNow `json:"now"`
	// Day is the shown day's high, low and weather.
	Day         todayDay    `json:"day"`
	Hours       []todayHour `json:"hours"`
	Attribution attribution `json:"attribution"`
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

type attribution struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type todayWarning struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	// Day is the day From falls on: in the evening a warning may be tonight's.
	Day string `json:"day"`
}

type todayRun struct {
	ID        string    `json:"id"`
	ThreadID  *string   `json:"threadId"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// forecast is the weather sidecar's answer, as far as the start page reads it.
type forecast struct {
	Hourly []struct {
		Time                     string  `json:"time"`
		Temperature              float64 `json:"temperature"`
		Code                     int     `json:"code"`
		PrecipitationProbability *int    `json:"precipitationProbability"`
	} `json:"hourly"`
	Daily []struct {
		Date                     string  `json:"date"`
		High                     float64 `json:"high"`
		Low                      float64 `json:"low"`
		Code                     int     `json:"code"`
		PrecipitationProbability *int    `json:"precipitationProbability"`
	} `json:"daily"`
	Attribution attribution `json:"attribution"`
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
		loc = placeZone(out.Location, user)
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
		if f, ok := s.forecastFor(ctx, p, loc); ok {
			if tomorrow {
				out.Weather, out.Warning = tomorrowForecast(f, local, shown)
			} else {
				out.Weather, out.Warning = todayForecast(f, local)
			}
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

// placeZone is the zone the place's times read in: its own, else the user's.
func placeZone(p *location.Place, user auth.User) *time.Location {
	for _, name := range []string{p.Timezone, user.Timezone} {
		if loc, err := time.LoadLocation(name); name != "" && err == nil {
			return loc
		}
	}
	return time.UTC
}

// sunOn is the sun on the shown day (a time on it, in loc), against the day
// before it; where the sun is now only when the shown day is today.
func sunOn(now, shown time.Time, p *location.Place, loc *time.Location) *todaySun {
	day := sun.On(shown, p.Latitude, p.Longitude, loc)
	// The local day before: 24 hours back from a time just past midnight
	// after a clock change can land two days back.
	before := sun.On(shown.In(loc).AddDate(0, 0, -1), p.Latitude, p.Longitude, loc)
	out := &todaySun{
		DayMinutes:   int(day.Length().Round(time.Minute) / time.Minute),
		DeltaMinutes: int((day.Length() - before.Length()).Round(time.Minute) / time.Minute),
		Polar:        day.Polar,
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

// forecastFor asks the weather sidecar for the place's forecast; ok is false
// when it cannot say.
func (s *server) forecastFor(ctx context.Context, p *location.Place, loc *time.Location) (forecast, bool) {
	ctx, cancel := context.WithTimeout(ctx, forecastTimeout)
	defer cancel()
	text, err := s.mcp.CallTool(ctx, forecastTool, map[string]any{
		"latitude": p.Latitude, "longitude": p.Longitude, "timezone": loc.String(), "days": 2,
	})
	if err != nil {
		slog.Warn("forecast failed", "err", err)
		return forecast{}, false
	}
	var f forecast
	if err := json.Unmarshal([]byte(text), &f); err != nil || len(f.Hourly) == 0 || len(f.Daily) == 0 {
		slog.Warn("forecast unreadable", "err", err)
		return forecast{}, false
	}
	return f, true
}

// todayForecast reads the start page's weather off the forecast from the
// current hour on: "now" between it and the next, five hours three apart,
// today's high and low, and a warning for the next hours. The contract does
// not promise the hours start now, so the current one is looked up; a
// forecast with no hour from now on is none.
func todayForecast(f forecast, now time.Time) (*todayWeather, *todayWarning) {
	current := now.Truncate(time.Hour).Format("2006-01-02T15:04")
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
	if !ok {
		// A daily list that does not name today: its first day stands in.
		day, _ = dayOf(f, time.Time{})
	}
	out := &todayWeather{
		Now:         &todayNow{Temperature: math.Round(temp*10) / 10, Code: first.Code},
		Day:         day,
		Hours:       []todayHour{},
		Attribution: f.Attribution,
	}
	for i := 1; i < len(f.Hourly) && len(out.Hours) < 5; i += 3 {
		out.Hours = append(out.Hours, hourOf(f, i))
	}
	return out, warningIn(f, 0, warningHours, now)
}

// tomorrowForecast reads tomorrow off the forecast: its high and low, five
// hours from tomorrowFrom three apart, and a warning from now (local) through
// tomorrow's daytime, tonight's included. A forecast that does not reach
// tomorrow, or whose days do not name it, is none: never today's numbers
// under tomorrow's name.
func tomorrowForecast(f forecast, local, tomorrow time.Time) (*todayWeather, *todayWarning) {
	day, ok := dayOf(f, tomorrow)
	if !ok {
		return nil, nil
	}
	date := tomorrow.Format(time.DateOnly)
	from, to := -1, -1
	for i, h := range f.Hourly {
		if len(h.Time) < 13 || h.Time[:10] != date {
			continue
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
		return nil, nil
	}
	out := &todayWeather{Day: day, Hours: []todayHour{}, Attribution: f.Attribution}
	for i := from; i <= to && len(out.Hours) < 5; i += 3 {
		out.Hours = append(out.Hours, hourOf(f, i))
	}
	tonight := currentHour(f, local)
	if tonight < 0 || tonight > from {
		tonight = from
	}
	return out, warningIn(f, tonight, to-tonight, local)
}

// currentHour is the index of the forecast's hour that holds local, or the
// first after it; -1 when the forecast ended before.
func currentHour(f forecast, local time.Time) int {
	current := local.Truncate(time.Hour).Format("2006-01-02T15:04")
	for i, h := range f.Hourly {
		if h.Time >= current {
			return i
		}
	}
	return -1
}

// dayOf is the forecast's day on date; ok is false when the forecast does
// not name it, the first day returned then. A zero date asks for the first.
func dayOf(f forecast, date time.Time) (todayDay, bool) {
	day, ok := f.Daily[0], date.IsZero()
	for _, d := range f.Daily {
		if !date.IsZero() && d.Date == date.Format(time.DateOnly) {
			day, ok = d, true
			break
		}
	}
	return todayDay{High: day.High, Low: day.Low, Code: day.Code, PrecipitationProbability: day.PrecipitationProbability}, ok
}

func hourOf(f forecast, i int) todayHour {
	h := f.Hourly[i]
	return todayHour{Time: clock(h.Time), Temperature: h.Temperature, Code: h.Code, PrecipitationProbability: h.PrecipitationProbability}
}

// warningIn is the first warning in the hours from start through start+span:
// precipitation likely enough, else frost; its Day says whether it falls on
// local's day or the next.
func warningIn(f forecast, start, span int, local time.Time) *todayWarning {
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
			return at(kind, h.Time)
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
