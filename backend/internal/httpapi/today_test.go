package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/weather"
)

// todayForecastJSON is a forecast from 08:00 on 9 November in Zurich through
// the next day: rain today from 17:00 to 22:00, tomorrow from 10:00 to 12:00.
func todayForecastJSON() string {
	var hours []string
	for i := range 40 {
		temp := 5.0 + float64(min(i, 6)) - float64(max(i-6, 0))*0.4
		code, chance := 3, 10
		if (i >= 9 && i <= 14) || (i >= 26 && i <= 28) {
			code, chance = 61, 80
		}
		rain := 0.0
		if code == 61 {
			rain = 0.5 + float64(i%3)*0.4
		}
		b, _ := json.Marshal(map[string]any{"time": time.Date(2026, 11, 9, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"),
			"temperature": temp, "code": code, "precipitationProbability": chance, "precipitation": rain,
			"windSpeed": 9 + float64(i), "windGusts": 20 + float64(i), "windDirection": 250,
			"apparentTemperature": temp - 3, "humidity": 80 + i%10, "sunshineMinutes": 30})
		hours = append(hours, string(b))
	}
	return `{"hourly":[` + strings.Join(hours, ",") + `],
"daily":[{"date":"2026-11-09","high":11,"low":3,"code":61,"precipitationProbability":80,"precipitationSum":4.2,
  "uvIndexMax":1.6,"sunshineHours":2.5,"windSpeedMax":24,"windGustsMax":45,"windDirection":240},
 {"date":"2026-11-10","high":9,"low":2,"code":61,"precipitationProbability":80,"precipitationSum":1.5,
  "uvIndexMax":1.2,"sunshineHours":0.5,"windSpeedMax":30,"windGustsMax":55,"windDirection":260},
 {"date":"2026-11-11","high":8,"low":1,"code":3,"precipitationProbability":20},
 {"date":"2026-11-12","high":7,"low":0,"code":71,"precipitationProbability":60},
 {"date":"2026-11-13","high":6,"low":-1,"code":0,"precipitationProbability":0},
 {"date":"2026-11-14","high":7,"low":0,"code":2,"precipitationProbability":10},
 {"date":"2026-11-15","high":9,"low":2,"code":3,"precipitationProbability":30}],
"attribution":{"text":"Weather data by Example","url":"https://weather.example/"}}`
}

// todayAirJSON is the air in Zurich from 08:00 on 9 November: birch
// moderate and grass low today, tomorrow only a trace of grass.
const todayAirJSON = `{"hourly":[{"time":"2026-11-09T08:00","aqi":18},{"time":"2026-11-09T09:00","aqi":45},
 {"time":"2026-11-09T21:00","aqi":30}],
"daily":[{"date":"2026-11-09","aqiMax":45,"pollen":{"birch":42.4,"grass":3,"olive":500,"alder":0.4}},
 {"date":"2026-11-10","aqiMax":64,"pollen":{"grass":0.2}}],
"attribution":{"text":"Air by Example","url":"https://air.example/"}}`

// airHours is the air's hours as "15:00=18,16:00=-".
func airHours(a *todayAir) string {
	out := make([]string, 0, len(a.Hours))
	for _, h := range a.Hours {
		v := "-"
		if h.AQI != nil {
			v = strconv.Itoa(*h.AQI)
		}
		out = append(out, h.Time+"="+v)
	}
	return strings.Join(out, ",")
}

func outlookDates(w *todayWeather) string {
	var dates []string
	for _, d := range w.Outlook {
		dates = append(dates, d.Date)
	}
	return strings.Join(dates, ",")
}

type todayFixture struct {
	*locationFixture
	schedules *schedule.Store
	handler   http.Handler
	// forecastDays is the days the last forecast call asked for.
	forecastDays any
	// forecastJSON answers the forecast; todayForecastJSON when empty.
	forecastJSON string
	// airErr fails the air quality call; airJSON answers it, todayAirJSON
	// when empty.
	airErr  error
	airJSON string
	// forecastErr fails the forecast.
	forecastErr error
}

// morning is 08:14 in Zurich on 9 November; dusk 19:30, evening 20:30.
var (
	morning = time.Date(2026, 11, 9, 7, 14, 0, 0, time.UTC)
	dusk    = time.Date(2026, 11, 9, 18, 30, 0, 0, time.UTC)
	evening = time.Date(2026, 11, 9, 19, 30, 0, 0, time.UTC)
)

func newTodayFixture(t *testing.T, withPlace bool) *todayFixture {
	t.Helper()
	f := newLocationFixture(t)
	ctx := context.Background()
	tf := &todayFixture{locationFixture: f}
	tf.schedules = schedule.NewStore(f.db)
	// Each visit derives the place in the background: it ends before the
	// test's database goes.
	bg := background.New(context.Background())
	f.deps.Background = bg
	t.Cleanup(func() { _ = bg.Stop(10 * time.Second) })
	geo := f.geo.call
	f.deps.MCP = fakeMCPService{
		Available: map[string]bool{weather.GeocodeTool: true, weather.ForecastTool: true, weather.AirQualityTool: true},
		CallFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
			if name == weather.AirQualityTool {
				if tf.airJSON != "" {
					return tf.airJSON, tf.airErr
				}
				return todayAirJSON, tf.airErr
			}
			if name == weather.ForecastTool {
				tf.forecastDays = args["days"]
				if tf.forecastErr != nil {
					return "", tf.forecastErr
				}
				if tf.forecastJSON != "" {
					return tf.forecastJSON, nil
				}
				return todayForecastJSON(), nil
			}
			return geo(ctx, name, args)
		},
	}
	f.deps.Schedules, f.deps.ScheduleLLM = tf.schedules, &fakeScheduleLLM{}
	if withPlace {
		f.addFact(t, "Lives Zürich")
		facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID)
		home := &location.Place{Name: "Zürich", Latitude: 47.3769, Longitude: 8.5417, Timezone: "Europe/Zurich", FactID: facts[0].ID, Query: "zürich|"}
		if err := f.places.Save(ctx, testUser.ID, location.HashFacts(facts), home, nil); err != nil {
			t.Fatal(err)
		}
	}
	tf.handler = todayHandler(newServer(f.deps), morning)
	return tf
}

// todayHandler serves GET /api/today for testUser at now.
func todayHandler(srv *server, now time.Time) http.Handler {
	srv.now = func() time.Time { return now }
	return auth.NewMiddleware(fakeSessionStore{userID: testUser.ID}, fakeUserStore{user: testUser, ok: true}).
		RequireAuth(http.HandlerFunc(srv.handleToday))
}

func (tf *todayFixture) get(t *testing.T) todayResponse {
	t.Helper()
	req := authenticatedRequest(http.MethodGet, "/api/today", "")
	req.Header.Set(timezoneHeader, "Europe/Zurich")
	rec := httptest.NewRecorder()
	tf.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("today = %d %s", rec.Code, rec.Body)
	}
	var out todayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func hourTimes(w *todayWeather) string {
	var times []string
	for _, h := range w.Hours {
		times = append(times, h.Time)
	}
	return strings.Join(times, ",")
}

func TestToday(t *testing.T) {
	tf := newTodayFixture(t, true)
	ctx := context.Background()
	for _, n := range []schedule.Notification{
		{Kind: schedule.KindScheduleRun, Title: "Tech briefing", Body: "Three new articles."},
		{Kind: schedule.KindScheduleDisabled, Title: "Off", Body: "disabled"},
		{Kind: schedule.KindScheduleRun, Title: "Read already", Body: "old"},
	} {
		if err := tf.schedules.CreateNotification(ctx, testUser.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	notes, _ := tf.schedules.ListNotifications(ctx, testUser.ID, 10)
	for _, n := range notes {
		if n.Title == "Read already" {
			_ = tf.schedules.MarkRead(ctx, testUser.ID, n.ID)
		}
	}

	got := tf.get(t)
	if got.Date != "2026-11-09" || got.Day != dayToday || got.AskLocation || got.Location == nil || got.Location.Name != "Zürich" {
		t.Fatalf("today = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Title != "Tech briefing" || got.Runs[0].Body != "Three new articles." {
		t.Fatalf("runs = %+v", got.Runs)
	}
	if s := got.Sun; s == nil || s.Rise == nil || s.Set == nil || !strings.HasPrefix(*s.Rise, "07:2") || !strings.HasPrefix(*s.Set, "16:5") ||
		s.Progress == nil || *s.Progress <= 0 || *s.Progress >= 0.2 {
		t.Fatalf("sun = %+v", got.Sun)
	}
	w := got.Weather
	if w == nil || w.Now == nil || w.Now.Temperature != 5.2 || w.Day.High != 11 {
		t.Fatalf("weather = %+v", w)
	}
	if hourTimes(w) != "09:00,12:00,15:00,18:00,21:00" {
		t.Fatalf("hours = %v", hourTimes(w))
	}
	// Details: the wind and air now, the rest the day's.
	d := w.Details
	if d == nil || *d.FeelsLike != 2 || *d.Humidity != 80 || *d.Wind != 9 || *d.Gusts != 20 || *d.WindDirection != 250 ||
		*d.UVMax != 1.6 || *d.PrecipitationSum != 4.2 || *d.SunshineHours != 2.5 {
		t.Fatalf("details = %+v", d)
	}
	// The rain hour by hour from now, twelve hours.
	if len(w.Rain) != 12 || w.Rain[0].Time != "08:00" || w.Rain[11].Time != "19:00" || *w.Rain[0].Precipitation != 0 ||
		*w.Rain[9].Precipitation != 0.5 || *w.Rain[9].Probability != 80 {
		t.Fatalf("rain = %+v", w.Rain)
	}
	// The five days after the shown one.
	if outlookDates(w) != "2026-11-10,2026-11-11,2026-11-12,2026-11-13,2026-11-14" || w.Outlook[2].Code != 71 ||
		w.Outlook[3].Low != -1 || *w.Outlook[2].PrecipitationProbability != 60 {
		t.Fatalf("outlook = %+v", w.Outlook)
	}
	if tf.forecastDays != 7 {
		t.Fatalf("asked %v days, want 7: the outlook on the evening reaches six days ahead", tf.forecastDays)
	}
	// The air now and today's pollen by their load classes: a trace is none,
	// a kind without classes (olive) is left out.
	a := got.Air
	if a == nil || *a.AQI != 18 || a.Level != "good" || a.Max {
		t.Fatalf("air = %+v", a)
	}
	// The index hour by hour from now, for the curve.
	if airHours(a) != "08:00=18,09:00=45,21:00=30" {
		t.Fatalf("air hours = %v", airHours(a))
	}
	if len(a.Pollen) != 2 || a.Pollen[0] != (todayPollen{Kind: "birch", Level: "moderate"}) ||
		a.Pollen[1] != (todayPollen{Kind: "grass", Level: "low"}) {
		t.Fatalf("pollen = %+v", a.Pollen)
	}
	if got.Warning == nil || got.Warning.Kind != "rain" || got.Warning.From != "17:00" || got.Warning.Day != dayToday ||
		got.Warning.Chance == nil || *got.Warning.Chance != 80 {
		t.Fatalf("warning = %+v", got.Warning)
	}
	// Tonight has its own warning only while the page shows tomorrow.
	if got.Tonight != nil {
		t.Fatalf("tonight by day = %+v", got.Tonight)
	}
	// The page turns to tomorrow at 20:00 at the place.
	if got.RefreshAt == nil || !got.RefreshAt.Equal(time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("refreshAt = %v, want 20:00 in Zurich", got.RefreshAt)
	}
}

// Before 20:00 the page still shows today.
func TestTodayAtDuskShowsToday(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.handler = todayHandler(newServer(tf.deps), dusk)
	got := tf.get(t)
	if got.Date != "2026-11-09" || got.Day != dayToday || got.Weather == nil || got.Weather.Now == nil {
		t.Fatalf("dusk = %+v", got)
	}
	if got.RefreshAt == nil || !got.RefreshAt.Equal(time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("refreshAt = %v, want 20:00 in Zurich", got.RefreshAt)
	}
}

// From 20:00 the page shows tomorrow: its date, weather and sun.
func TestTodayInTheEveningShowsTomorrow(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.handler = todayHandler(newServer(tf.deps), evening)
	got := tf.get(t)
	if got.Date != "2026-11-10" || got.Day != dayTomorrow {
		t.Fatalf("evening = %+v", got)
	}
	w := got.Weather
	if w == nil || w.Now != nil || w.Day.High != 9 || hourTimes(w) != "06:00,09:00,12:00,15:00,18:00" {
		t.Fatalf("tomorrow's weather = %+v", w)
	}
	// Tomorrow's details are its day's: no air now, the wind at its strongest.
	if d := w.Details; d == nil || d.FeelsLike != nil || d.Humidity != nil || *d.Wind != 30 || *d.Gusts != 55 ||
		*d.WindDirection != 260 || *d.UVMax != 1.2 || *d.PrecipitationSum != 1.5 || *d.SunshineHours != 0.5 {
		t.Fatalf("tomorrow's details = %+v", w.Details)
	}
	if len(w.Rain) != 17 || w.Rain[0].Time != "06:00" || w.Rain[16].Time != "22:00" {
		t.Fatalf("tomorrow's rain = %+v", w.Rain)
	}
	if outlookDates(w) != "2026-11-11,2026-11-12,2026-11-13,2026-11-14,2026-11-15" {
		t.Fatalf("outlook after tomorrow = %v", outlookDates(w))
	}
	// Tomorrow's air is its worst, no pollen worth a class.
	if a := got.Air; a == nil || *a.AQI != 64 || a.Level != "poor" || !a.Max || a.Pollen == nil || len(a.Pollen) != 0 ||
		a.Hours == nil || len(a.Hours) != 0 {
		t.Fatalf("tomorrow's air = %+v", got.Air)
	}
	// It is still raining tonight: that is tonight's, apart from tomorrow's.
	if n := got.Tonight; n == nil || n.Kind != "rain" || n.From != "20:00" || n.Day != dayToday || n.Chance == nil || *n.Chance != 80 {
		t.Fatalf("tonight's warning = %+v", got.Tonight)
	}
	if got.Warning == nil || got.Warning.Kind != "rain" || got.Warning.From != "10:00" || got.Warning.Day != dayTomorrow {
		t.Fatalf("tomorrow's warning = %+v", got.Warning)
	}
	if s := got.Sun; s == nil || s.Rise == nil || !strings.HasPrefix(*s.Rise, "07:2") || s.Progress != nil {
		t.Fatalf("tomorrow's sun = %+v", got.Sun)
	}
	// At midnight the shown tomorrow becomes today.
	if got.RefreshAt == nil || !got.RefreshAt.Equal(time.Date(2026, 11, 9, 23, 0, 0, 0, time.UTC)) {
		t.Fatalf("refreshAt = %v, want midnight in Zurich", got.RefreshAt)
	}
	// Late, with tonight dry: no warning for tonight, tomorrow's stays.
	var f weather.Forecast
	if err := json.Unmarshal([]byte(todayForecastJSON()), &f); err != nil {
		t.Fatal(err)
	}
	zurich, _ := time.LoadLocation("Europe/Zurich")
	late := time.Date(2026, 11, 9, 23, 10, 0, 0, zurich)
	if _, w, n := tomorrowForecast(f, late, late.AddDate(0, 0, 1)); w == nil || w.From != "10:00" || w.Day != dayTomorrow || n != nil {
		t.Fatalf("late: tomorrow's warning = %+v, tonight's = %+v", w, n)
	}
}

// Tonight ends at midnight: rain at 02:00 or frost at 05:00 falls while
// everyone sleeps, no pill; rain from 06:00 is tomorrow's.
func TestTomorrowWarnsOfTonightApart(t *testing.T) {
	build := func(wet func(at time.Time) bool) weather.Forecast {
		var f weather.Forecast
		for i := range 20 {
			at := time.Date(2026, 11, 9, 21+i, 0, 0, 0, time.UTC)
			temp, code, chance := 3.0, 3, 10
			if at.Hour() == 5 {
				temp = -1
			}
			if wet(at) {
				code, chance = 61, 90
			}
			f.Hourly = append(f.Hourly, weather.Hour{Time: at.Format("2006-01-02T15:04"), Temperature: temp, Code: code,
				PrecipitationProbability: &chance})
		}
		f.Daily = []weather.Day{{Date: "2026-11-09"}, {Date: "2026-11-10", High: 5, Low: -1, Code: 61}}
		return f
	}
	local := time.Date(2026, 11, 9, 21, 15, 0, 0, time.UTC)
	small := build(func(at time.Time) bool { return at.Day() == 10 && (at.Hour() == 2 || at.Hour() >= 6) })
	_, w, n := tomorrowForecast(small, local, local.AddDate(0, 0, 1))
	if n != nil {
		t.Fatalf("the small hours made tonight's warning %+v", n)
	}
	if w == nil || w.Kind != "rain" || w.From != "06:00" || w.Day != dayTomorrow || w.Chance == nil || *w.Chance != 90 {
		t.Fatalf("tomorrow's warning = %+v", w)
	}
	late := build(func(at time.Time) bool { return at.Day() == 9 && at.Hour() == 23 })
	if _, _, n := tomorrowForecast(late, local, local.AddDate(0, 0, 1)); n == nil || n.Kind != "rain" || n.From != "23:00" ||
		n.Day != dayToday || n.Chance == nil || *n.Chance != 90 {
		t.Fatalf("tonight's warning = %+v", n)
	}
}

func TestTodayWithoutAPlace(t *testing.T) {
	tf := newTodayFixture(t, false)
	got := tf.get(t)
	// The facts were read (none name a place): ask.
	if !got.AskLocation || got.Location != nil || got.Weather != nil || got.Sun != nil || got.Runs == nil {
		t.Fatalf("no place = %+v", got)
	}
}

// While the facts have not been read since they changed, the memory may know
// the place already: no question, or an answer would add a second home fact.
func TestTodayDoesNotAskWhileTheFactsAreUnread(t *testing.T) {
	tf := newTodayFixture(t, false)
	tf.addFact(t, "Lives Bern")
	// The derivation the visit starts waits on the model until the answer is
	// out, as a real one would.
	answered := make(chan struct{})
	tf.llm.during = func() { <-answered }
	got := tf.get(t)
	close(answered)
	if got.AskLocation {
		t.Fatalf("asked while a derivation is pending: %+v", got)
	}
}

// A derivation that keeps failing must not hide the question for good: once
// it failed for the facts as they are, the page asks.
func TestTodayAsksWhenTheDerivationFails(t *testing.T) {
	tf := newTodayFixture(t, false)
	tf.addFact(t, "Likes tea")
	tf.llm.err = errors.New("model down")
	srv := newServer(tf.deps)
	tf.handler = todayHandler(srv, morning)
	if err := srv.resolveLocation(context.Background(), testUser); err == nil {
		t.Fatal("the derivation did not fail")
	}
	if got := tf.get(t); !got.AskLocation {
		t.Fatalf("no question after a failed derivation: %+v", got)
	}
}

// A weather server that answers only the basics (an mcp.json replacement)
// costs the cards it has no values for, never the weather.
func TestTodayFromABasicWeatherServer(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.forecastJSON = `{"hourly":[{"time":"2026-11-09T08:00","temperature":5,"code":3},
 {"time":"2026-11-09T09:00","temperature":6,"code":3}],
"daily":[{"date":"2026-11-09","high":11,"low":3,"code":3},{"date":"2026-11-10","high":9,"low":2,"code":3}]}`
	w := tf.get(t).Weather
	if w == nil || w.Now == nil {
		t.Fatalf("weather = %+v", w)
	}
	if w.Details != nil || w.Rain != nil {
		t.Fatalf("details = %+v, rain = %+v; want none without their values", w.Details, w.Rain)
	}
	if outlookDates(w) != "2026-11-10" {
		t.Fatalf("outlook = %v, want the one day after today it has", outlookDates(w))
	}
}

// The air is optional: a failing call or a server without the tool costs the
// air card, never the weather.
func TestTodayWithoutTheAir(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.airErr = errors.New("air down")
	if got := tf.get(t); got.Air != nil || got.Weather == nil {
		t.Fatalf("failed air: air %+v, weather %+v", got.Air, got.Weather)
	}
	tf = newTodayFixture(t, true)
	svc := tf.deps.MCP.(fakeMCPService)
	delete(svc.Available, weather.AirQualityTool)
	if got := tf.get(t); got.Air != nil || got.Weather == nil {
		t.Fatalf("no air tool: air %+v, weather %+v", got.Air, got.Weather)
	}
}

// Without the weather the air has no tile to show on: left out.
func TestTodayAirNeedsTheWeather(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.forecastErr = errors.New("forecast down")
	if got := tf.get(t); got.Weather != nil || got.Air != nil {
		t.Fatalf("weather %+v, air %+v; want neither", got.Weather, got.Air)
	}
}

// Another server's air: a fractional index is rounded, an hour without one
// falls to the next, and no pollen at all is unknown, never "hardly any".
func TestTodayAirFromAnotherServer(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.airJSON = `{"hourly":[{"time":"2026-11-09T08:00"},{"time":"2026-11-09T09:00","aqi":44.6}],
"daily":[{"date":"2026-11-09","aqiMax":44.6}]}`
	a := tf.get(t).Air
	if a == nil || *a.AQI != 45 || a.Level != "moderate" || a.Pollen != nil || airHours(a) != "08:00=-,09:00=45" {
		t.Fatalf("air = %+v", a)
	}
	// An index only hours from now is not now; with no pollen there is
	// nothing to show.
	tf.airJSON = `{"hourly":[{"time":"2026-11-09T08:00"},{"time":"2026-11-09T09:00"},{"time":"2026-11-09T10:00"},
 {"time":"2026-11-09T11:00","aqi":30}],"daily":[{"date":"2026-11-09"}]}`
	if a := tf.get(t).Air; a != nil {
		t.Fatalf("air = %+v, want none", a)
	}
}

// A forecast whose days do not name tomorrow has no tomorrow: never today's
// numbers under tomorrow's name.
func TestTomorrowNeedsItsOwnDay(t *testing.T) {
	var f weather.Forecast
	if err := json.Unmarshal([]byte(todayForecastJSON()), &f); err != nil {
		t.Fatal(err)
	}
	f.Daily = f.Daily[:1]
	if weather, warning, tonight := tomorrowForecast(f, time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)); weather != nil || warning != nil || tonight != nil {
		t.Fatalf("tomorrow from today's day: %+v, %+v, %+v", weather, warning, tonight)
	}
}

// The contract does not promise the forecast starts now: the current hour is
// looked up, and a forecast that ended is none.
func TestTodayForecastFindsTheCurrentHour(t *testing.T) {
	var f weather.Forecast
	if err := json.Unmarshal([]byte(todayForecastJSON()), &f); err != nil {
		t.Fatal(err)
	}
	// 14:30: the forecast starts at 08:00.
	weather, warning := todayForecast(f, time.Date(2026, 11, 9, 14, 30, 0, 0, time.UTC))
	if weather == nil || weather.Now.Temperature != 10.8 || weather.Hours[0].Time != "15:00" || warning == nil || warning.From != "17:00" {
		t.Fatalf("at 14:30: %+v, %+v", weather, warning)
	}
	// 07:30, before the first hour: no interpolation into it.
	if weather, _ := todayForecast(f, time.Date(2026, 11, 9, 7, 30, 0, 0, time.UTC)); weather == nil || weather.Now.Temperature != 5 {
		t.Fatalf("at 07:30: %+v", weather)
	}
	if weather, warning := todayForecast(f, time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)); weather != nil || warning != nil {
		t.Fatalf("after the forecast: %+v, %+v", weather, warning)
	}
	// A forecast that does not reach tomorrow's daytime has no tomorrow.
	if weather, warning, tonight := tomorrowForecast(f, time.Date(2026, 11, 10, 19, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)); weather != nil || warning != nil || tonight != nil {
		t.Fatalf("beyond the forecast: %+v, %+v, %+v", weather, warning, tonight)
	}
}

func TestTodayWithoutTheWeather(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.deps.MCP = fakeMCPService{}
	tf.handler = todayHandler(newServer(tf.deps), morning)
	got := tf.get(t)
	// No lookup: no places, nothing asked, the page still answers.
	if got.AskLocation || got.Location != nil || got.Weather != nil || len(got.Runs) != 0 {
		t.Fatalf("weather off = %+v", got)
	}
}

func TestTodayForecastWarnings(t *testing.T) {
	now := time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC)
	mk := func(codes []int, temps []float64, chance int) weather.Forecast {
		var f weather.Forecast
		for i := range codes {
			c := chance
			f.Hourly = append(f.Hourly, weather.Hour{Time: time.Date(2026, 11, 9, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"),
				Temperature: temps[i], Code: codes[i], PrecipitationProbability: &c})
		}
		f.Daily = append(f.Daily, weather.Day{Date: "2026-11-08", High: 1, Low: -2, Code: 3})
		return f
	}
	if _, w := todayForecast(mk([]int{3, 3, 71}, []float64{2, 1, 0}, 80), now); w == nil || w.Kind != "snow" || w.From != "10:00" {
		t.Fatalf("snow = %+v", w)
	}
	if _, w := todayForecast(mk([]int{3, 3, 3}, []float64{2, -1, -2}, 80), now); w == nil || w.Kind != "frost" || w.From != "09:00" {
		t.Fatalf("frost = %+v", w)
	}
	if _, w := todayForecast(mk([]int{3, 95}, []float64{2, 3}, 40), now); w != nil {
		t.Fatalf("a storm at 40%% is no warning: %+v", w)
	}
	if _, w := todayForecast(mk([]int{3, 95}, []float64{2, 3}, 60), now); w == nil || w.Kind != "storm" {
		t.Fatalf("storm = %+v", w)
	}
	// No day matching today: the first one stands in for the range, never
	// for the details.
	stand := mk([]int{3}, []float64{2}, 0)
	uv := 5.0
	stand.Daily[0].UVIndexMax = &uv
	if weather, _ := todayForecast(stand, now); weather.Day.High != 1 || weather.Day.Low != -2 || weather.Now.Temperature != 2 ||
		weather.Details != nil {
		t.Fatalf("weather = %+v", weather)
	}
	if clock("garbled") != "garbled" {
		t.Fatal("an unreadable time is passed through")
	}
}
