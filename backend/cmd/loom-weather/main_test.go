package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

func vars(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{"WEATHER_TOKEN": testToken}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.forecastURL != defaultForecastURL || cfg.geocodingURL != defaultGeocodingURL ||
		cfg.airQualityURL != defaultAirQualityURL {
		t.Fatalf("defaults %+v", cfg)
	}
	// No token: the sidecar starts and refuses every tool call.
	cfg, err = loadConfig(vars(map[string]string{}))
	if err != nil || cfg.token != "" {
		t.Fatalf("no token: %+v, %v", cfg, err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer ")
	newServer(cfg, http.DefaultClient, "test").ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token configured, empty bearer: %d", rec.Code)
	}
	for name, values := range map[string]map[string]string{
		"short token":       {"WEATHER_TOKEN": "short"},
		"bad forecast url":  {"WEATHER_TOKEN": testToken, "WEATHER_FORECAST_URL": "api.example"},
		"bad geocoding url": {"WEATHER_TOKEN": testToken, "WEATHER_GEOCODING_URL": "ftp://x"},
		"bad air url":       {"WEATHER_TOKEN": testToken, "WEATHER_AIR_QUALITY_URL": "x"},
	} {
		if _, err := loadConfig(vars(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestServerNeedsTheToken(t *testing.T) {
	h := newServer(config{token: testToken, forecastURL: defaultForecastURL, geocodingURL: defaultGeocodingURL}, http.DefaultClient, "test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
}

const hourlyReply = `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich","utc_offset_seconds":3600,
"hourly":{"time":["2026-11-09T08:00","2026-11-09T09:00","2026-11-09T10:00","2026-11-10T08:00","2026-11-11T08:00"],
 "temperature_2m":[5.1,6.2,null,4,3],"weather_code":[3,2,1,61,0],"precipitation_probability":[10,null,0,80,0],
 "precipitation":[0,null,0,1.2,0],"wind_speed_10m":[8.5,9,10,20,5],"wind_gusts_10m":[20.1,null,22,45,12],
 "wind_direction_10m":[250,251,null,270,90],"apparent_temperature":[2.4,null,4,0.5,1],
 "relative_humidity_2m":[87,null,80,95,70],"uv_index":[0.4,null,1.5,0.2,0.9]}}`

// dailyReply starts a day early, as a reply kept over midnight does.
const dailyReply = `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich","utc_offset_seconds":3600,
"daily":{"time":["2026-11-08","2026-11-09","2026-11-10","2026-11-11","2026-11-12"],
 "temperature_2m_max":[10,11.2,9,8,7],"temperature_2m_min":[2,3.1,null,1,0],"weather_code":[1,3,61,2,0],
 "precipitation_probability_max":[0,10,80,null,5],"precipitation_sum":[0,0.3,null,0,0],
 "uv_index_max":[1,1.6,null,2,2],"sunshine_duration":[3600,16200,null,0,25000],
 "wind_speed_10m_max":[10,12.5,null,8,6],"wind_gusts_10m_max":[25,31,null,20,15],
 "wind_direction_10m_dominant":[200,248,null,90,100]}}`

// airReply is two days of air from midnight in Zurich: birch the first day
// only, grass every other hour, no alder at all.
func airReply() string {
	var times, aqi, birch, grass, nulls []string
	for i := range 48 {
		times = append(times, fmt.Sprintf(`"2026-11-%02dT%02d:00"`, 9+i/24, i%24))
		aqi = append(aqi, fmt.Sprint(10+i))
		nulls = append(nulls, "null")
		b, g := "null", "0"
		if i < 24 {
			b = "20"
		}
		if i%2 == 1 {
			g = "10"
		}
		birch, grass = append(birch, b), append(grass, g)
	}
	j := func(v []string) string { return "[" + strings.Join(v, ",") + "]" }
	return `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich","utc_offset_seconds":3600,"hourly":{"time":` + j(times) +
		`,"european_aqi":` + j(aqi) + `,"alder_pollen":` + j(nulls) + `,"birch_pollen":` + j(birch) +
		`,"grass_pollen":` + j(grass) + `,"mugwort_pollen":` + j(nulls) + `,"olive_pollen":` + j(nulls) +
		`,"ragweed_pollen":` + j(nulls) + `}}`
}

// fakeProvider answers the forecast and geocoding paths, counts the calls
// and can be slowed or failed.
type fakeProvider struct {
	hourly, daily, air, lookups atomic.Int32
	status                      int
	gate                        chan struct{}
	mu                          sync.Mutex
	queries                     []string
}

// query is the provider's last query whose parameters hold part.
func (f *fakeProvider) query(part string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.queries) - 1; i >= 0; i-- {
		if strings.Contains(f.queries[i], part) {
			return f.queries[i]
		}
	}
	return ""
}

func (f *fakeProvider) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.gate != nil {
			<-f.gate
		}
		if f.status != 0 {
			http.Error(w, `{"error":true,"reason":"nope"}`, f.status)
			return
		}
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.RawQuery)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/forecast" && r.URL.Query().Has("hourly"):
			f.hourly.Add(1)
			_, _ = w.Write([]byte(hourlyReply))
		case r.URL.Path == "/v1/forecast" && r.URL.Query().Has("daily"):
			f.daily.Add(1)
			_, _ = w.Write([]byte(dailyReply))
		case r.URL.Path == "/v1/air-quality":
			f.air.Add(1)
			_, _ = w.Write([]byte(airReply()))
		case r.URL.Path == "/v1/search":
			f.lookups.Add(1)
			_, _ = w.Write([]byte(`{"results":[{"name":"Baden","latitude":47.47,"longitude":8.3,"timezone":"Europe/Zurich","country":"Schweiz","country_code":"CH","admin1":"Aargau"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// loomService is loom's own MCP client on a sidecar in front of the fake.
func loomService(t *testing.T, provider string, now func() time.Time) *mcp.Service {
	t.Helper()
	cfg := config{token: testToken, forecastURL: provider, geocodingURL: provider, airQualityURL: provider}
	srv := httptest.NewServer(newServerAt(cfg, http.DefaultClient, "test", now))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.WeatherServer: mcp.WeatherServerConfig(srv.URL, testToken),
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

var (
	forecastName = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherForecastTool)
	geocodeName  = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherGeocodeTool)
	airName      = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherAirQualityTool)
)

func TestAirQuality(t *testing.T) {
	fake := &fakeProvider{}
	// 08:30 in Zurich.
	clock := time.Date(2026, 11, 9, 7, 30, 0, 0, time.UTC)
	var mu sync.Mutex
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	svc := loomService(t, fake.server(t).URL, now)
	args := map[string]any{"latitude": 47.377, "longitude": 8.542, "timezone": "Europe/Zurich"}
	res, err := svc.CallToolResult(context.Background(), airName, args)
	if err != nil {
		t.Fatal(err)
	}
	var got AirQuality
	if err := json.Unmarshal([]byte(res.Text), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, res.Text)
	}
	// The hours from the current one, a day of them.
	if len(got.Hourly) != 24 || got.Hourly[0].Time != "2026-11-09T08:00" || *got.Hourly[0].AQI != 18 {
		t.Fatalf("hours = %+v", got.Hourly)
	}
	// Two days by default; pollen as each day's mean, a kind without values
	// (or a day without any) left out.
	if len(got.Daily) != 2 {
		t.Fatalf("days = %+v", got.Daily)
	}
	d0, d1 := got.Daily[0], got.Daily[1]
	if d0.Date != "2026-11-09" || *d0.AQIMax != 33 || len(d0.Pollen) != 2 || d0.Pollen["birch"] != 20 || d0.Pollen["grass"] != 5 {
		t.Fatalf("first day = %+v", d0)
	}
	if d1.Date != "2026-11-10" || *d1.AQIMax != 57 || len(d1.Pollen) != 1 || d1.Pollen["grass"] != 5 {
		t.Fatalf("second day = %+v", d1)
	}
	if strings.Contains(res.Text, "alder") || got.Attribution.Text == "" || len(res.Sources) != 1 ||
		!strings.Contains(got.Attribution.Text, "CAMS") {
		t.Fatalf("answer = %s, sources %+v", res.Text, res.Sources)
	}
	query := fake.query("european_aqi")
	for _, part := range []string{"latitude=47.377", "forecast_days=4", "timezone=Europe%2FZurich", "birch_pollen"} {
		if !strings.Contains(query, part) {
			t.Fatalf("provider query %q lacks %q", query, part)
		}
	}
	// Kept until the next six-hour mark (UTC): the models behind it run once
	// or twice a day.
	for _, step := range []struct {
		to   time.Time
		want int32
	}{
		{time.Date(2026, 11, 9, 11, 59, 0, 0, time.UTC), 1},
		{time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), 2},
	} {
		mu.Lock()
		clock = step.to
		mu.Unlock()
		if _, err := svc.CallTool(context.Background(), airName, args); err != nil || fake.air.Load() != step.want {
			t.Fatalf("at %s: %d calls, %v; want %d", step.to.Format("15:04"), fake.air.Load(), err, step.want)
		}
	}
	for name, a := range map[string]map[string]any{
		"days":     {"latitude": 1, "longitude": 2, "days": 5},
		"no point": {"latitude": 1},
		"timezone": {"latitude": 1, "longitude": 2, "timezone": "../x"},
	} {
		if _, err := svc.CallTool(context.Background(), airName, a); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestForecast(t *testing.T) {
	fake := &fakeProvider{}
	// 08:30 in Zurich: the 08:00 hour is the current one.
	at := func() time.Time { return time.Date(2026, 11, 9, 7, 30, 0, 0, time.UTC) }
	svc := loomService(t, fake.server(t).URL, at)
	res, err := svc.CallToolResult(context.Background(), forecastName, map[string]any{"latitude": 47.37689, "longitude": 8.54169, "timezone": "Europe/Zurich"})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Text
	hourlyQuery, dailyQuery := fake.query("hourly="), fake.query("daily=")
	var got Forecast
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	// The licence's credit is cited as a source too.
	if len(res.Sources) != 1 || res.Sources[0].URL != got.Attribution.URL {
		t.Fatalf("sources = %+v", res.Sources)
	}
	// A POSIX-style zone is a zone, and 2.0 days are 2.
	if _, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 40.7, "longitude": -74, "timezone": "EST5EDT", "days": 2.0}); err != nil {
		t.Fatalf("EST5EDT: %v", err)
	}
	// An hour later the 08:00 row is gone from the answer.
	later := loomService(t, fake.server(t).URL, func() time.Time { return at().Add(time.Hour) })
	text2, err := later.CallTool(context.Background(), forecastName, map[string]any{"latitude": 47.37689, "longitude": 8.54169, "timezone": "Europe/Zurich"})
	if err != nil || strings.Contains(text2, "2026-11-09T08:00") || !strings.Contains(text2, "2026-11-09T09:00") {
		t.Fatalf("an hour later: %s, %v", text2, err)
	}
	// Two days by default: the hours of today and tomorrow, an hour or day
	// missing a value it cannot do without left out, a day before today's
	// dropped; a missing value stays missing, never 0.
	if got.Timezone != "Europe/Zurich" || len(got.Hourly) != 3 || len(got.Daily) != 1 {
		t.Fatalf("forecast = %+v", got)
	}
	h := got.Hourly[0]
	if h.Time != "2026-11-09T08:00" || h.Temperature != 5.1 || h.Code != 3 || *h.PrecipitationProbability != 10 ||
		*h.WindSpeed != 8.5 || *h.Precipitation != 0 || *h.WindGusts != 20.1 || *h.WindDirection != 250 ||
		*h.ApparentTemperature != 2.4 || *h.Humidity != 87 || *h.UVIndex != 0.4 {
		t.Fatalf("first hour = %+v", h)
	}
	if !strings.Contains(text, `{"time":"2026-11-09T09:00","temperature":6.2,"code":2,"windSpeed":9,"windDirection":251}`) {
		t.Fatalf("an unknown value was sent: %s", text)
	}
	d := got.Daily[0]
	if d.Date != "2026-11-09" || d.High != 11.2 || d.Low != 3.1 || d.Code != 3 || *d.PrecipitationProbability != 10 ||
		*d.PrecipitationSum != 0.3 || *d.UVIndexMax != 1.6 || *d.SunshineHours != 4.5 || *d.WindSpeedMax != 12.5 ||
		*d.WindGustsMax != 31 || *d.WindDirection != 248 {
		t.Fatalf("days = %+v", got.Daily)
	}
	if got.Attribution.Text == "" || !strings.HasPrefix(got.Attribution.URL, "https://") {
		t.Fatalf("attribution = %+v", got.Attribution)
	}
	// The hours are fetched for three days, the days for seven, whatever
	// the call asked: one fetch of each serves every call for the place.
	for query, parts := range map[string][]string{
		hourlyQuery: {"latitude=47.377", "longitude=8.542", "forecast_days=3", "timezone=Europe%2FZurich"},
		dailyQuery:  {"latitude=47.377", "longitude=8.542", "forecast_days=7", "timezone=Europe%2FZurich"},
	} {
		for _, part := range parts {
			if !strings.Contains(query, part) {
				t.Fatalf("provider query %q lacks %q", query, part)
			}
		}
	}
	if strings.Contains(hourlyQuery, "daily=") || strings.Contains(dailyQuery, "hourly=") {
		t.Fatalf("hours and days asked together: %q, %q", hourlyQuery, dailyQuery)
	}
}

// days bounds the answer: the hours cover at most the first three days, the
// days all asked for that the provider has.
func TestForecastDays(t *testing.T) {
	fake := &fakeProvider{}
	at := func() time.Time { return time.Date(2026, 11, 9, 7, 30, 0, 0, time.UTC) }
	svc := loomService(t, fake.server(t).URL, at)
	for days, want := range map[int][2]int{1: {2, 1}, 3: {4, 2}, 7: {4, 3}} {
		text, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 47.377, "longitude": 8.542, "days": days})
		if err != nil {
			t.Fatal(err)
		}
		var got Forecast
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Hourly) != want[0] || len(got.Daily) != want[1] {
			t.Errorf("days %d: %d hours, %d days; want %v", days, len(got.Hourly), len(got.Daily), want)
		}
	}
	if fake.hourly.Load() != 1 || fake.daily.Load() != 1 {
		t.Fatalf("provider calls for 1, 3 and 7 days = %d hourly, %d daily; want one each", fake.hourly.Load(), fake.daily.Load())
	}
}

// Every loom user in one place shares one provider request: concurrent
// misses fold into one, and the answer is kept until the top of the hour.
func TestForecastIsFetchedOncePerPlaceAndHour(t *testing.T) {
	fake := &fakeProvider{gate: make(chan struct{})}
	var mu sync.Mutex
	clock := time.Date(2026, 11, 9, 8, 14, 0, 0, time.UTC)
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	svc := loomService(t, fake.server(t).URL, now)
	args := map[string]any{"latitude": 47.377, "longitude": 8.542}

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for range 50 {
		wg.Go(func() {
			if _, err := svc.CallTool(context.Background(), forecastName, args); err != nil {
				errs <- err
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(fake.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if h, d := fake.hourly.Load(), fake.daily.Load(); h != 1 || d != 1 {
		t.Fatalf("provider calls for 50 concurrent requests = %d hourly, %d daily; want 1 each", h, d)
	}

	// The hours are kept until the top of the hour, the days until the next
	// three-hour mark (UTC): the models behind them run every three hours.
	for _, step := range []struct {
		to            string
		hourly, daily int32
	}{
		{"08:59", 1, 1},
		{"09:01", 2, 2}, // 09:00 is a three-hour mark
		{"10:01", 3, 2},
		{"11:59", 4, 2},
		{"12:00", 5, 3},
	} {
		mu.Lock()
		hm, _ := time.Parse("15:04", step.to)
		clock = time.Date(2026, 11, 9, hm.Hour(), hm.Minute(), 0, 0, time.UTC)
		mu.Unlock()
		if _, err := svc.CallTool(context.Background(), forecastName, args); err != nil {
			t.Fatal(err)
		}
		if h, d := fake.hourly.Load(), fake.daily.Load(); h != step.hourly || d != step.daily {
			t.Fatalf("at %s: %d hourly, %d daily; want %d, %d", step.to, h, d, step.hourly, step.daily)
		}
	}
}

func TestGeocode(t *testing.T) {
	fake := &fakeProvider{}
	svc := loomService(t, fake.server(t).URL, time.Now)
	args := map[string]any{"query": "  Baden ", "country": "ch", "language": "de"}
	text, err := svc.CallTool(context.Background(), geocodeName, args)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Places []Place }
	if err := json.Unmarshal([]byte(text), &got); err != nil || len(got.Places) != 1 {
		t.Fatalf("places = %s, %v", text, err)
	}
	if got.Places[0] != (Place{Name: "Baden", Region: "Aargau", Country: "Schweiz", CountryCode: "CH", Latitude: 47.47, Longitude: 8.3, Timezone: "Europe/Zurich"}) {
		t.Fatalf("place = %+v", got.Places[0])
	}
	query := fake.query("name=")
	for _, part := range []string{"name=Baden", "countryCode=CH", "language=de", fmt.Sprintf("count=%d", maxPlaces)} {
		if !strings.Contains(query, part) {
			t.Fatalf("provider query %q lacks %q", query, part)
		}
	}
	// The same lookup is kept.
	if _, err := svc.CallTool(context.Background(), geocodeName, map[string]any{"query": "baden", "country": "CH", "language": "de"}); err != nil || fake.lookups.Load() != 1 {
		t.Fatalf("repeat lookup: calls = %d, %v", fake.lookups.Load(), err)
	}
}

// A bad argument or a failing provider is an error result, never a crash,
// and a failure is not kept.
func TestErrorsAreToolErrors(t *testing.T) {
	fake := &fakeProvider{status: http.StatusBadRequest}
	svc := loomService(t, fake.server(t).URL, time.Now)
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"provider":      {forecastName, map[string]any{"latitude": 1, "longitude": 2}, "400"},
		"no point":      {forecastName, map[string]any{"latitude": 1}, "required"},
		"off the globe": {forecastName, map[string]any{"latitude": 91, "longitude": 0}, "within"},
		"days":          {forecastName, map[string]any{"latitude": 1, "longitude": 2, "days": 9}, "days"},
		"half a day":    {forecastName, map[string]any{"latitude": 1, "longitude": 2, "days": 1.5}, "whole number"},
		"timezone":      {forecastName, map[string]any{"latitude": 1, "longitude": 2, "timezone": "../etc"}, "IANA"},
		"unknown":       {forecastName, map[string]any{"lat": 1}, "invalid arguments"},
		"lookup":        {geocodeName, map[string]any{"query": "Baden"}, "400"},
		"no query":      {geocodeName, map[string]any{"query": " "}, "query is required"},
		"country":       {geocodeName, map[string]any{"query": "x", "country": "CHE"}, "alpha-2"},
		"language":      {geocodeName, map[string]any{"query": "x", "language": "deu"}, "ISO 639-1"},
	} {
		got, err := svc.CallToolResult(context.Background(), tc.tool, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %+v, %v; want error with %q", name, got, err, tc.want)
		}
	}
	fake.status = 0
	if _, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 1, "longitude": 2}); err != nil {
		t.Fatalf("after a failure the next call asks again: %v", err)
	}
}

func TestUnreachableAndUnreadableProvider(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	svc := loomService(t, dead.URL, time.Now)
	if _, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 1, "longitude": 2}); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("dead provider: %v", err)
	}
	for body, want := range map[string]string{
		"<html>":                 "unreadable",
		`{"hourly":{"time":[]}}`: "no forecast",
		`{"x":"` + strings.Repeat("x", maxProviderBody) + `"}`: "MiB",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		svc := loomService(t, srv.URL, time.Now)
		if _, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 3, "longitude": 4}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("reply %.20q: %v, want %q", body, err, want)
		}
		srv.Close()
	}
}

func TestCacheCallerGivesUp(t *testing.T) {
	c := newCache[int](time.Now, 10)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(ctx, "k", nextHour, func(context.Context) (int, error) { <-release; return 1, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller: %v", err)
	}
	close(release)
	// The fetch it started still lands for the next caller.
	v, err := c.get(context.Background(), "k", nextHour, func(context.Context) (int, error) { return 2, nil })
	if err != nil || (v != 1 && v != 2) {
		t.Fatalf("next caller = %d, %v", v, err)
	}
}

func TestCachePrunes(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newCache[int](func() time.Time { return clock }, 2)
	fetch := func(context.Context) (int, error) { return 1, nil }
	for _, k := range []string{"a", "b", "c"} {
		if _, err := c.get(context.Background(), k, nextHour, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.entries) > 2 {
		t.Fatalf("entries = %d, want at most the limit", len(c.entries))
	}
	// "c" is all that is left, and it expires: at the limit, the expired go
	// first and the live entry stays.
	clock = clock.Add(2 * time.Hour)
	for _, k := range []string{"d", "e"} {
		if _, err := c.get(context.Background(), k, afterGeocodeTTL, fetch); err != nil {
			t.Fatal(err)
		}
	}
	_, hasC := c.entries["c"]
	_, hasD := c.entries["d"]
	if hasC || !hasD || len(c.entries) != 2 {
		t.Fatalf("entries = %v, want the expired c pruned and d kept", c.entries)
	}
}

// loom addresses the contract by its own copy of the names: they must match
// the server's.
func TestContractNamesMatchLoom(t *testing.T) {
	if mcp.WeatherServer != serverName || mcp.WeatherForecastTool != forecastTool || mcp.WeatherGeocodeTool != geocodeTool ||
		mcp.WeatherAirQualityTool != airQualityTool {
		t.Fatal("loom's weather contract names drifted from loom-weather's")
	}
}
