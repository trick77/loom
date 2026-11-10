package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/mcp"
)

var reverseName = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherReverseGeocodeTool)

// fakeReverse answers the reverse lookup like the map provider and the
// place-name search like the weather provider: two towns named Baden, the
// German one first.
type fakeReverse struct {
	reverses, searches atomic.Int32
	// failSearches fails that many weather searches.
	failSearches atomic.Int32
	// address is the reverse lookup's answer; "" answers it found nothing.
	address string
	mu      sync.Mutex
	queries []string
	agents  []string
}

func (f *fakeReverse) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.Path+"?"+r.URL.RawQuery)
		f.agents = append(f.agents, r.UserAgent())
		f.mu.Unlock()
		switch r.URL.Path {
		case "/reverse":
			f.reverses.Add(1)
			if f.address == "" {
				_, _ = w.Write([]byte(`{"error":"Unable to geocode"}`))
				return
			}
			_, _ = w.Write([]byte(`{"lat":"47.4801","lon":"8.3158","address":` + f.address + `}`))
		case "/v1/search":
			f.searches.Add(1)
			if f.failSearches.Load() > 0 {
				f.failSearches.Add(-1)
				http.Error(w, `{"error":true}`, http.StatusBadGateway)
				return
			}
			towns := map[string][]string{
				"Baden": {
					`{"name":"Baden-Baden","latitude":48.76,"longitude":8.24,"timezone":"Europe/Berlin","country":"Deutschland","country_code":"DE","admin1":"Baden-Württemberg"}`,
					`{"name":"Baden","latitude":47.47,"longitude":8.31,"timezone":"Europe/Zurich","country":"Schweiz","country_code":"CH","admin1":"Aargau"}`,
				},
				"Ennetbaden": {
					`{"name":"Ennetbaden","latitude":47.48,"longitude":8.32,"timezone":"Europe/Zurich","country":"Schweiz","country_code":"CH","admin1":"Aargau"}`,
				},
			}[r.URL.Query().Get("name")]
			var results []string
			for _, town := range towns {
				if cc := r.URL.Query().Get("countryCode"); cc == "" || strings.Contains(town, `"country_code":"`+cc+`"`) {
					results = append(results, town)
				}
			}
			if len(results) == 0 {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeReverse) query(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.queries) - 1; i >= 0; i-- {
		if strings.HasPrefix(f.queries[i], path) {
			return f.queries[i]
		}
	}
	return ""
}

func reverseService(t *testing.T, provider, reverseURL string) *mcp.Service {
	t.Helper()
	cfg := config{token: testToken, forecastURL: provider, geocodingURL: provider, airQualityURL: provider, reverseURL: reverseURL}
	srv := httptest.NewServer(newServerAt(cfg, http.DefaultClient, "test", time.Now))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.WeatherServer: mcp.WeatherServerConfig(srv.URL, testToken),
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func noReverseWait(t *testing.T) {
	t.Helper()
	was := reverseInterval
	reverseInterval = 0
	t.Cleanup(func() { reverseInterval = was })
}

func reversePlaces(t *testing.T, text string) reverseAnswer {
	t.Helper()
	var got reverseAnswer
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return got
}

// A point is named by the map provider's town and answered as the weather
// provider's match nearest to it: the Swiss Baden, though the search lists
// the German one first.
func TestReverseGeocodeNamesTheNearestTown(t *testing.T) {
	noReverseWait(t)
	fake := &fakeReverse{address: `{"town":"Baden","county":"Bezirk Baden","state":"Aargau","country":"Schweiz","country_code":"ch"}`}
	url := fake.server(t).URL
	svc := reverseService(t, url, url)
	text, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": 47.47712, "longitude": 8.30851, "language": "de"})
	if err != nil {
		t.Fatal(err)
	}
	got := reversePlaces(t, text)
	want := Place{Name: "Baden", Region: "Aargau", Country: "Schweiz", CountryCode: "CH", Latitude: 47.47, Longitude: 8.31, Timezone: "Europe/Zurich"}
	if len(got.Places) != 1 || got.Places[0] != want {
		t.Fatalf("places = %+v", got.Places)
	}
	if !strings.Contains(got.Attribution.Text, "OpenStreetMap") || got.Attribution.URL == "" {
		t.Fatalf("attribution = %+v", got.Attribution)
	}
	// The point leaves rounded to about a kilometre, asked at town level,
	// under a name of its own.
	q := fake.query("/reverse")
	for _, part := range []string{"lat=47.48", "lon=8.31", "zoom=13", "accept-language=de", "format=jsonv2"} {
		if !strings.Contains(q, part) {
			t.Fatalf("reverse query %q lacks %q", q, part)
		}
	}
	if strings.Contains(q, "47.477") {
		t.Fatalf("reverse query %q carries the exact point", q)
	}
	if s := fake.query("/v1/search"); !strings.Contains(s, "countryCode=CH") || !strings.Contains(s, "count=20") {
		t.Fatalf("search %q not narrowed to the country, or too few matches", s)
	}
	fake.mu.Lock()
	agent := fake.agents[0]
	fake.mu.Unlock()
	if !strings.HasPrefix(agent, "loom-weather/") {
		t.Fatalf("user agent %q", agent)
	}
	// A point within the same kilometre is kept.
	if _, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": 47.4789, "longitude": 8.3112, "language": "de"}); err != nil || fake.reverses.Load() != 1 {
		t.Fatalf("repeat: %d reverse calls, %v", fake.reverses.Load(), err)
	}
}

// A village the search knows by its own name is that village; a name the
// search does not know falls back to the next wider one.
func TestReverseGeocodeTriesWiderNames(t *testing.T) {
	noReverseWait(t)
	fake := &fakeReverse{address: `{"village":"Nowhere","municipality":"Ennetbaden","country_code":"ch"}`}
	url := fake.server(t).URL
	svc := reverseService(t, url, url)
	text, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": 47.48, "longitude": 8.32})
	if err != nil {
		t.Fatal(err)
	}
	if got := reversePlaces(t, text); len(got.Places) != 1 || got.Places[0].Name != "Ennetbaden" {
		t.Fatalf("places = %+v", got.Places)
	}
	if q := fake.query("/reverse"); !strings.Contains(q, "accept-language=en") {
		t.Fatalf("default language: %q", q)
	}
}

// A weather search that fails moves on to the next name; when none answers,
// the call fails, and the retry spends no second map request.
func TestReverseGeocodeSearchFailures(t *testing.T) {
	noReverseWait(t)
	fake := &fakeReverse{address: `{"village":"Ennetbaden","town":"Baden","country_code":"ch"}`}
	fake.failSearches.Store(1)
	url := fake.server(t).URL
	svc := reverseService(t, url, url)
	text, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": 47.47, "longitude": 8.31})
	if err != nil {
		t.Fatal(err)
	}
	if got := reversePlaces(t, text); len(got.Places) != 1 || got.Places[0].Name != "Baden" {
		t.Fatalf("after a failed first name: %+v", got.Places)
	}

	fake = &fakeReverse{address: `{"town":"Baden","country_code":"ch"}`}
	fake.failSearches.Store(1)
	url = fake.server(t).URL
	svc = reverseService(t, url, url)
	args := map[string]any{"latitude": 47.47, "longitude": 8.31}
	if _, err := svc.CallTool(context.Background(), reverseName, args); err == nil {
		t.Fatal("a failed search answered no place")
	}
	text, err = svc.CallTool(context.Background(), reverseName, args)
	if err != nil || len(reversePlaces(t, text).Places) != 1 || fake.reverses.Load() != 1 {
		t.Fatalf("retry: %s, %v, %d map requests", text, err, fake.reverses.Load())
	}
}

// A lookup by name answers as many matches as asked, up to the most.
func TestGeocodeCount(t *testing.T) {
	fake := &fakeReverse{}
	url := fake.server(t).URL
	svc := reverseService(t, url, url)
	if _, err := svc.CallTool(context.Background(), geocodeName, map[string]any{"query": "Baden", "count": 20.0}); err != nil {
		t.Fatal(err)
	}
	if q := fake.query("/v1/search"); !strings.Contains(q, "count=20") {
		t.Fatalf("search %q", q)
	}
	for _, bad := range []any{21, 2.5, -1} {
		if _, err := svc.CallToolResult(context.Background(), geocodeName, map[string]any{"query": "Baden", "count": bad}); err == nil || !strings.Contains(err.Error(), "count") {
			t.Errorf("count %v: %v", bad, err)
		}
	}
}

// Nothing there (the sea) or only a namesake far away: no place, not an error.
func TestReverseGeocodeFindsNothing(t *testing.T) {
	noReverseWait(t)
	for name, address := range map[string]string{
		"sea":      "",
		"far away": `{"town":"Baden","country_code":"at"}`,
	} {
		fake := &fakeReverse{address: address}
		url := fake.server(t).URL
		svc := reverseService(t, url, url)
		// Off the coast of Portugal, or Baden bei Wien named for a Swiss point.
		lat, lon := 38.0, -12.0
		if address != "" {
			lat, lon = 47.47, 8.31
		}
		text, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": lat, "longitude": lon})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := reversePlaces(t, text); len(got.Places) != 0 {
			t.Fatalf("%s: places = %+v", name, got.Places)
		}
	}
}

func TestReverseGeocodeErrors(t *testing.T) {
	noReverseWait(t)
	fake := &fakeReverse{}
	url := fake.server(t).URL
	svc := reverseService(t, url, url)
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"no point":      {map[string]any{"latitude": 1}, "required"},
		"off the globe": {map[string]any{"latitude": 91, "longitude": 0}, "within"},
		"language":      {map[string]any{"latitude": 1, "longitude": 2, "language": "deu"}, "ISO 639-1"},
	} {
		if _, err := svc.CallToolResult(context.Background(), reverseName, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v; want %q", name, err, tc.want)
		}
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	svc = reverseService(t, url, dead.URL)
	if _, err := svc.CallTool(context.Background(), reverseName, map[string]any{"latitude": 1, "longitude": 2}); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("dead provider: %v", err)
	}
}

// Turned off, the sidecar has no reverse lookup: loom shows no button.
func TestReverseGeocodeOff(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{"WEATHER_TOKEN": testToken}))
	if err != nil || cfg.reverseURL != defaultReverseURL {
		t.Fatalf("default: %q, %v", cfg.reverseURL, err)
	}
	cfg, err = loadConfig(vars(map[string]string{"WEATHER_TOKEN": testToken, "WEATHER_REVERSE_URL": "off"}))
	if err != nil || cfg.reverseURL != "" {
		t.Fatalf("off: %q, %v", cfg.reverseURL, err)
	}
	if _, err := loadConfig(vars(map[string]string{"WEATHER_TOKEN": testToken, "WEATHER_REVERSE_URL": "x"})); err == nil {
		t.Fatal("bad reverse url accepted")
	}
	svc := reverseService(t, "http://127.0.0.1:1", "")
	if svc.HasTool(reverseName) {
		t.Fatal("reverse lookup offered while off")
	}
}

// The map provider allows one request a second from the whole sidecar; a
// queue longer than most is refused, not grown.
func TestReverseLimiterSpacesCalls(t *testing.T) {
	var l limiter
	start := time.Now()
	for range 3 {
		if err := l.wait(context.Background(), 20*time.Millisecond, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("three calls in %v", elapsed)
	}
	var full limiter
	if err := full.wait(context.Background(), time.Hour, 90*time.Minute); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The second's turn is an hour out, within the queue: it waits, until
	// its caller gives up.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := full.wait(ctx, time.Hour, 90*time.Minute); err == nil || errors.Is(err, errMapBusy) {
		t.Fatalf("a canceled wait: %v", err)
	}
	// The third's would be two hours out: refused at once.
	if err := full.wait(context.Background(), time.Hour, 90*time.Minute); !errors.Is(err, errMapBusy) {
		t.Fatalf("past the queue: %v", err)
	}
}
