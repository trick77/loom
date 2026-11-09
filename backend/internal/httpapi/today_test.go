package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/schedule"
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
		b, _ := json.Marshal(map[string]any{"time": time.Date(2026, 11, 9, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"),
			"temperature": temp, "code": code, "precipitationProbability": chance})
		hours = append(hours, string(b))
	}
	return `{"hourly":[` + strings.Join(hours, ",") + `],
"daily":[{"date":"2026-11-09","high":11,"low":3,"code":61,"precipitationProbability":80},
 {"date":"2026-11-10","high":9,"low":2,"code":61,"precipitationProbability":80}],
"attribution":{"text":"Weather data by Example","url":"https://weather.example/"}}`
}

type todayFixture struct {
	*locationFixture
	schedules *schedule.Store
	handler   http.Handler
}

// morning is 08:14 in Zurich on 9 November; evening 18:30.
var (
	morning = time.Date(2026, 11, 9, 7, 14, 0, 0, time.UTC)
	evening = time.Date(2026, 11, 9, 17, 30, 0, 0, time.UTC)
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
		Available: map[string]bool{geocodeTool: true, forecastTool: true},
		CallFunc: func(ctx context.Context, name string, args map[string]any) (string, error) {
			if name == forecastTool {
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
		s.DeltaMinutes > -1 || s.DeltaMinutes < -4 || s.Progress == nil || *s.Progress <= 0 || *s.Progress >= 0.2 {
		t.Fatalf("sun = %+v", got.Sun)
	}
	w := got.Weather
	if w == nil || w.Now == nil || w.Now.Temperature != 5.2 || w.Day.High != 11 || w.Attribution.URL != "https://weather.example/" {
		t.Fatalf("weather = %+v", w)
	}
	if hourTimes(w) != "09:00,12:00,15:00,18:00,21:00" {
		t.Fatalf("hours = %v", hourTimes(w))
	}
	if got.Warning == nil || got.Warning.Kind != "rain" || got.Warning.From != "17:00" || got.Warning.Day != dayToday {
		t.Fatalf("warning = %+v", got.Warning)
	}
	// The page turns to tomorrow at 18:00 at the place.
	if got.RefreshAt == nil || !got.RefreshAt.Equal(time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("refreshAt = %v, want 18:00 in Zurich", got.RefreshAt)
	}
}

// From 18:00 the page shows tomorrow: its date, weather and sun.
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
	// It is still raining tonight: that comes first, as tonight's.
	if got.Warning == nil || got.Warning.Kind != "rain" || got.Warning.From != "18:00" || got.Warning.Day != dayToday {
		t.Fatalf("tonight's warning = %+v", got.Warning)
	}
	if s := got.Sun; s == nil || s.Rise == nil || !strings.HasPrefix(*s.Rise, "07:2") || s.Progress != nil || s.DeltaMinutes > -1 {
		t.Fatalf("tomorrow's sun = %+v", got.Sun)
	}
	// At midnight the shown tomorrow becomes today.
	if got.RefreshAt == nil || !got.RefreshAt.Equal(time.Date(2026, 11, 9, 23, 0, 0, 0, time.UTC)) {
		t.Fatalf("refreshAt = %v, want midnight in Zurich", got.RefreshAt)
	}
	// Late, with tonight dry: tomorrow's rain is the warning.
	var f forecast
	if err := json.Unmarshal([]byte(todayForecastJSON()), &f); err != nil {
		t.Fatal(err)
	}
	zurich, _ := time.LoadLocation("Europe/Zurich")
	late := time.Date(2026, 11, 9, 23, 10, 0, 0, zurich)
	if _, w := tomorrowForecast(f, late, late.AddDate(0, 0, 1)); w == nil || w.From != "10:00" || w.Day != dayTomorrow {
		t.Fatalf("tomorrow's warning = %+v", w)
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

// A forecast whose days do not name tomorrow has no tomorrow: never today's
// numbers under tomorrow's name.
func TestTomorrowNeedsItsOwnDay(t *testing.T) {
	var f forecast
	if err := json.Unmarshal([]byte(todayForecastJSON()), &f); err != nil {
		t.Fatal(err)
	}
	f.Daily = f.Daily[:1]
	if weather, warning := tomorrowForecast(f, time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)); weather != nil || warning != nil {
		t.Fatalf("tomorrow from today's day: %+v, %+v", weather, warning)
	}
}

// Just past midnight after the clocks went forward, the day before is still
// the local day before, not two days back.
func TestSunOnAcrossAClockChange(t *testing.T) {
	zurich, _ := time.LoadLocation("Europe/Zurich")
	p := &location.Place{Latitude: 47.3769, Longitude: 8.5417}
	at := time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC) // 00:30 CEST on the 30th
	got := sunOn(at, at.In(zurich), p, zurich)
	if got.DeltaMinutes < 2 || got.DeltaMinutes > 4 {
		t.Fatalf("delta = %d min, want one day's change (about 3)", got.DeltaMinutes)
	}
}

// The contract does not promise the forecast starts now: the current hour is
// looked up, and a forecast that ended is none.
func TestTodayForecastFindsTheCurrentHour(t *testing.T) {
	var f forecast
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
	if weather, warning := tomorrowForecast(f, time.Date(2026, 11, 10, 19, 0, 0, 0, time.UTC), time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)); weather != nil || warning != nil {
		t.Fatalf("beyond the forecast: %+v, %+v", weather, warning)
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
	mk := func(codes []int, temps []float64, chance int) forecast {
		var f forecast
		for i := range codes {
			c := chance
			f.Hourly = append(f.Hourly, struct {
				Time                     string  `json:"time"`
				Temperature              float64 `json:"temperature"`
				Code                     int     `json:"code"`
				PrecipitationProbability *int    `json:"precipitationProbability"`
			}{Time: time.Date(2026, 11, 9, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"), Temperature: temps[i], Code: codes[i], PrecipitationProbability: &c})
		}
		f.Daily = append(f.Daily, struct {
			Date                     string  `json:"date"`
			High                     float64 `json:"high"`
			Low                      float64 `json:"low"`
			Code                     int     `json:"code"`
			PrecipitationProbability *int    `json:"precipitationProbability"`
		}{Date: "2026-11-08", High: 1, Low: -2, Code: 3})
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
	// No day matching today: the first one stands in.
	if weather, _ := todayForecast(mk([]int{3}, []float64{2}, 0), now); weather.Day.High != 1 || weather.Now.Temperature != 2 {
		t.Fatalf("weather = %+v", weather)
	}
	if clock("garbled") != "garbled" {
		t.Fatal("an unreadable time is passed through")
	}
}
