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
	if cfg.addr != defaultAddr || cfg.forecastURL != defaultForecastURL || cfg.geocodingURL != defaultGeocodingURL {
		t.Fatalf("defaults %+v", cfg)
	}
	for name, values := range map[string]map[string]string{
		"no token":          {},
		"short token":       {"WEATHER_TOKEN": "short"},
		"bad forecast url":  {"WEATHER_TOKEN": testToken, "WEATHER_FORECAST_URL": "api.example"},
		"bad geocoding url": {"WEATHER_TOKEN": testToken, "WEATHER_GEOCODING_URL": "ftp://x"},
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

const forecastReply = `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich",
"hourly":{"time":["2026-11-09T08:00","2026-11-09T09:00","2026-11-09T10:00"],
 "temperature_2m":[5.1,6.2,null],"weather_code":[3,2,1],"precipitation_probability":[10,null,0],"wind_speed_10m":[8.5,9,10]},
"daily":{"time":["2026-11-09","2026-11-10"],"temperature_2m_max":[11.2,9],"temperature_2m_min":[3.1,null],
 "weather_code":[3,61],"precipitation_probability_max":[10,80]}}`

// fakeProvider answers the forecast and geocoding paths, counts the calls
// and can be slowed or failed.
type fakeProvider struct {
	forecasts, lookups atomic.Int32
	status             int
	gate               chan struct{}
	query              string
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
		switch r.URL.Path {
		case "/v1/forecast":
			f.forecasts.Add(1)
			f.query = r.URL.RawQuery
			_, _ = w.Write([]byte(forecastReply))
		case "/v1/search":
			f.lookups.Add(1)
			f.query = r.URL.RawQuery
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
	cfg := config{token: testToken, forecastURL: provider, geocodingURL: provider}
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
)

func TestForecast(t *testing.T) {
	fake := &fakeProvider{}
	svc := loomService(t, fake.server(t).URL, time.Now)
	text, err := svc.CallTool(context.Background(), forecastName, map[string]any{"latitude": 47.37689, "longitude": 8.54169, "timezone": "Europe/Zurich"})
	if err != nil {
		t.Fatal(err)
	}
	var got Forecast
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	// An hour or day missing a value it cannot do without is left out; a
	// missing probability reads 0.
	if got.Timezone != "Europe/Zurich" || len(got.Hourly) != 2 || len(got.Daily) != 1 {
		t.Fatalf("forecast = %+v", got)
	}
	if got.Hourly[0] != (Hour{Time: "2026-11-09T08:00", Temperature: 5.1, Code: 3, PrecipitationProbability: 10, WindSpeed: 8.5}) ||
		got.Hourly[1].PrecipitationProbability != 0 {
		t.Fatalf("hours = %+v", got.Hourly)
	}
	if got.Daily[0] != (Day{Date: "2026-11-09", High: 11.2, Low: 3.1, Code: 3, PrecipitationProbability: 10}) {
		t.Fatalf("days = %+v", got.Daily)
	}
	if got.Attribution.Text == "" || !strings.HasPrefix(got.Attribution.URL, "https://") {
		t.Fatalf("attribution = %+v", got.Attribution)
	}
	for _, part := range []string{"latitude=47.377", "longitude=8.542", "forecast_days=2", "timezone=Europe%2FZurich"} {
		if !strings.Contains(fake.query, part) {
			t.Fatalf("provider query %q lacks %q", fake.query, part)
		}
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
	if n := fake.forecasts.Load(); n != 1 {
		t.Fatalf("provider calls for 50 concurrent requests = %d, want 1", n)
	}

	mu.Lock()
	clock = clock.Add(45 * time.Minute) // 08:59
	mu.Unlock()
	if _, err := svc.CallTool(context.Background(), forecastName, args); err != nil || fake.forecasts.Load() != 1 {
		t.Fatalf("within the hour: calls = %d, %v", fake.forecasts.Load(), err)
	}
	mu.Lock()
	clock = clock.Add(2 * time.Minute) // 09:01
	mu.Unlock()
	if _, err := svc.CallTool(context.Background(), forecastName, args); err != nil || fake.forecasts.Load() != 2 {
		t.Fatalf("after the hour: calls = %d, %v", fake.forecasts.Load(), err)
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
	for _, part := range []string{"name=Baden", "countryCode=CH", "language=de", fmt.Sprintf("count=%d", maxPlaces)} {
		if !strings.Contains(fake.query, part) {
			t.Fatalf("provider query %q lacks %q", fake.query, part)
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
	if mcp.WeatherServer != serverName || mcp.WeatherForecastTool != forecastTool || mcp.WeatherGeocodeTool != geocodeTool {
		t.Fatal("loom's weather contract names drifted from loom-weather's")
	}
}
