package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/schedule"
)

// seenUsers keeps the day each user last saw "today".
type seenUsers struct {
	fakeUserStore
	mu   sync.Mutex
	seen map[string]string
}

func (u *seenUsers) MarkTodaySeen(_ context.Context, id, day string) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.seen[id] == day {
		return false, nil
	}
	u.seen[id] = day
	return true, nil
}

// todayForecastJSON is a forecast from 08:00 in Zurich: rain from 17:00.
func todayForecastJSON() string {
	var hours []string
	temps := []float64{5, 7, 8, 9, 10, 10, 11, 10, 9, 8, 7, 6, 5, 4, 3}
	for i, t := range temps {
		code, chance := 3, 10
		if i >= 9 {
			code, chance = 61, 70
		}
		b, _ := json.Marshal(map[string]any{"time": time.Date(2026, 11, 9, 8+i, 0, 0, 0, time.UTC).Format("2006-01-02T15:04"),
			"temperature": t, "code": code, "precipitationProbability": chance})
		hours = append(hours, string(b))
	}
	return `{"hourly":[` + strings.Join(hours, ",") + `],
"daily":[{"date":"2026-11-09","high":11,"low":3,"code":61,"precipitationProbability":70}],
"attribution":{"text":"Weather data by Example","url":"https://weather.example/"}}`
}

type todayFixture struct {
	*locationFixture
	schedules *schedule.Store
	users     *seenUsers
	handler   http.Handler
}

func newTodayFixture(t *testing.T, withPlace bool) *todayFixture {
	t.Helper()
	f := newLocationFixture(t)
	ctx := context.Background()
	tf := &todayFixture{locationFixture: f, users: &seenUsers{fakeUserStore: fakeUserStore{user: testUser, ok: true}, seen: map[string]string{}}}
	tf.schedules = schedule.NewStore(f.db)
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
	f.deps.Users = tf.users
	f.deps.Schedules, f.deps.ScheduleLLM = tf.schedules, &fakeScheduleLLM{}
	if withPlace {
		f.addFact(t, "Lives Zürich")
		facts, _ := f.chat.ListActiveUserFacts(ctx, testUser.ID)
		home := &location.Place{Name: "Zürich", Latitude: 47.3769, Longitude: 8.5417, Timezone: "Europe/Zurich", FactID: facts[0].ID, Query: "zürich|"}
		if err := f.places.Save(ctx, testUser.ID, location.HashFacts(facts), home, nil); err != nil {
			t.Fatal(err)
		}
	}
	tf.handler = todayHandler(newServer(f.deps))
	return tf
}

// todayHandler serves GET /api/today for testUser at 08:14 in Zurich.
func todayHandler(srv *server) http.Handler {
	srv.now = func() time.Time { return time.Date(2026, 11, 9, 7, 14, 0, 0, time.UTC) }
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
	if got.Date != "2026-11-09" || got.View != "tiles" || got.AskLocation || got.Location == nil || got.Location.Name != "Zürich" {
		t.Fatalf("today = %+v", got)
	}
	if len(got.Runs) != 1 || got.Runs[0].Title != "Tech briefing" || got.Runs[0].Body != "Three new articles." {
		t.Fatalf("runs = %+v", got.Runs)
	}
	if s := got.Sun; s == nil || s.Rise == nil || s.Set == nil || !strings.HasPrefix(*s.Rise, "07:2") || !strings.HasPrefix(*s.Set, "16:5") ||
		s.DeltaMinutes > -1 || s.DeltaMinutes < -4 || s.Progress <= 0 || s.Progress >= 0.2 {
		t.Fatalf("sun = %+v", got.Sun)
	}
	w := got.Weather
	if w == nil || w.Now.Temperature != 5.5 || w.Today.High != 11 || w.Attribution.URL != "https://weather.example/" {
		t.Fatalf("weather = %+v", w)
	}
	var times []string
	for _, h := range w.Hours {
		times = append(times, h.Time)
	}
	if strings.Join(times, ",") != "09:00,12:00,15:00,18:00,21:00" {
		t.Fatalf("hours = %v", times)
	}
	if got.Warning == nil || got.Warning.Kind != "rain" || got.Warning.From != "17:00" {
		t.Fatalf("warning = %+v", got.Warning)
	}
	// Later that day: one line.
	if again := tf.get(t); again.View != "line" {
		t.Fatalf("second visit view = %q", again.View)
	}
}

func TestTodayWithoutAPlace(t *testing.T) {
	tf := newTodayFixture(t, false)
	got := tf.get(t)
	if !got.AskLocation || got.Location != nil || got.Weather != nil || got.Sun != nil || got.Runs == nil {
		t.Fatalf("no place = %+v", got)
	}
}

func TestTodayWithoutTheWeather(t *testing.T) {
	tf := newTodayFixture(t, true)
	tf.deps.MCP = fakeMCPService{}
	tf.handler = todayHandler(newServer(tf.deps))
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
	if weather, _ := todayForecast(mk([]int{3}, []float64{2}, 0), now); weather.Today.High != 1 || weather.Now.Temperature != 2 {
		t.Fatalf("weather = %+v", weather)
	}
	if clock("garbled") != "garbled" {
		t.Fatal("an unreadable time is passed through")
	}
}
