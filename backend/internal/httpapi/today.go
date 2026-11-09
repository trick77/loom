package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
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
	// warningHours is how far ahead a warning looks.
	warningHours = 12
	// warningRainChance is the chance of precipitation from which an hour
	// with rain, snow or a storm is a warning.
	warningRainChance = 50
)

type todayResponse struct {
	Date        string          `json:"date"`
	View        string          `json:"view"`
	Location    *location.Place `json:"location"`
	AskLocation bool            `json:"askLocation"`
	Sun         *todaySun       `json:"sun"`
	Weather     *todayWeather   `json:"weather"`
	Warning     *todayWarning   `json:"warning"`
	Runs        []todayRun      `json:"runs"`
}

type todaySun struct {
	Rise         *string   `json:"rise"`
	Set          *string   `json:"set"`
	DayMinutes   int       `json:"dayMinutes"`
	DeltaMinutes int       `json:"deltaMinutes"`
	Polar        sun.Polar `json:"polar"`
	// Progress is where the sun is between rise (0) and set (1) now.
	Progress float64 `json:"progress"`
}

type todayWeather struct {
	Now         todayNow    `json:"now"`
	Today       todayDay    `json:"today"`
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

// handleToday is the start page's "today": the user's place, its weather and
// sun, and the schedule results not read yet. The first call of a day that
// has weather to show says "tiles", later ones "line". Every part is
// optional: one that fails is left out, never the page.
func (s *server) handleToday(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	user.Timezone = s.rememberTimezone(ctx, user, r)
	now := s.clock()
	out := todayResponse{Date: userToday(user, now), View: "line", Runs: s.todayRuns(ctx, user)}
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
		out.Location, out.AskLocation = place, err == nil && place == nil
	}
	if p := out.Location; p != nil {
		loc := placeZone(p, user)
		out.Sun = sunOn(now, p, loc)
		if f, ok := s.forecastFor(ctx, p, loc); ok {
			out.Weather, out.Warning = todayForecast(f, now.In(loc))
		}
	}
	// Only a visit that can show tiles uses up the day's: one without weather
	// (the place still being derived, the forecast failing) or from a phone,
	// which always shows the line (?compact=1), leaves them for later.
	if out.Weather != nil && s.users != nil && r.URL.Query().Get("compact") != "1" {
		if first, err := s.users.MarkTodaySeen(ctx, user.ID, out.Date); err != nil {
			slog.Warn("mark today seen failed", "user_id", user.ID, "err", err)
		} else if first {
			out.View = "tiles"
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// todayRuns is the schedule results the user has not read, newest first.
func (s *server) todayRuns(ctx context.Context, user auth.User) []todayRun {
	runs := []todayRun{}
	if s.schedules == nil {
		return runs
	}
	// Kept 30 days: a window this wide still finds an unread run behind an
	// hourly schedule's read ones.
	notes, err := s.schedules.ListNotifications(ctx, user.ID, 1000)
	if err != nil {
		slog.Warn("list notifications failed", "user_id", user.ID, "err", err)
		return runs
	}
	for _, n := range notes {
		if n.Kind != schedule.KindScheduleRun || n.ReadAt != nil {
			continue
		}
		runs = append(runs, todayRun{ID: n.ID, ThreadID: n.ThreadID, Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt})
		if len(runs) == todayRuns {
			break
		}
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

func sunOn(now time.Time, p *location.Place, loc *time.Location) *todaySun {
	today := sun.On(now, p.Latitude, p.Longitude, loc)
	// The local day before: 24 hours back from a time just past midnight
	// after a clock change can land two days back.
	yesterday := sun.On(now.In(loc).AddDate(0, 0, -1), p.Latitude, p.Longitude, loc)
	out := &todaySun{
		DayMinutes:   int(today.Length().Round(time.Minute) / time.Minute),
		DeltaMinutes: int((today.Length() - yesterday.Length()).Round(time.Minute) / time.Minute),
		Polar:        today.Polar,
	}
	if today.Polar != sun.NotPolar {
		return out
	}
	rise, set := today.Rise.Format("15:04"), today.Set.Format("15:04")
	out.Rise, out.Set = &rise, &set
	if length := today.Length(); length > 0 {
		out.Progress = math.Round(float64(now.Sub(today.Rise))/float64(length)*1000) / 1000
	}
	return out
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
	start := -1
	for i, h := range f.Hourly {
		if h.Time >= current {
			start = i
			break
		}
	}
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
	day := f.Daily[0]
	for _, d := range f.Daily {
		if d.Date == now.Format(time.DateOnly) {
			day = d
			break
		}
	}
	out := &todayWeather{
		Now:         todayNow{Temperature: math.Round(temp*10) / 10, Code: first.Code},
		Today:       todayDay{High: day.High, Low: day.Low, Code: day.Code, PrecipitationProbability: day.PrecipitationProbability},
		Hours:       []todayHour{},
		Attribution: f.Attribution,
	}
	for i := 1; i < len(f.Hourly) && len(out.Hours) < 5; i += 3 {
		h := f.Hourly[i]
		out.Hours = append(out.Hours, todayHour{Time: clock(h.Time), Temperature: h.Temperature, Code: h.Code, PrecipitationProbability: h.PrecipitationProbability})
	}
	var warning *todayWarning
	for i, h := range f.Hourly {
		if i > warningHours {
			break
		}
		if kind := precipitation(h.Code); kind != "" && h.PrecipitationProbability != nil && *h.PrecipitationProbability >= warningRainChance {
			warning = &todayWarning{Kind: kind, From: clock(h.Time)}
			break
		}
		if warning == nil && h.Temperature <= 0 {
			warning = &todayWarning{Kind: "frost", From: clock(h.Time)}
		}
	}
	return out, warning
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
