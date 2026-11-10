package weather

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/mcp"
)

type fakeCaller struct {
	mu    sync.Mutex
	calls []string
	reply map[string]string
	err   error
	// airErr fails the air alone.
	airErr error
	// lastArgs are the last call's arguments.
	lastArgs map[string]any
}

func (f *fakeCaller) HasTool(name string) bool {
	_, ok := f.reply[name]
	return ok
}

func (f *fakeCaller) CallToolResult(_ context.Context, name string, args map[string]any) (mcp.CallResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	f.lastArgs = args
	if f.err != nil {
		return mcp.CallResult{}, f.err
	}
	if name == AirQualityTool && f.airErr != nil {
		return mcp.CallResult{}, f.airErr
	}
	return mcp.CallResult{Text: f.reply[name], Sources: []mcp.Source{{URL: "https://credit.example/"}}}, nil
}

func TestGetForecastReadsTheContract(t *testing.T) {
	c := &fakeCaller{reply: map[string]string{ForecastTool: `{"hourly":[{"time":"2026-11-09T09:00","temperature":6,"code":2,
		"sunshineMinutes":40}],"daily":[{"date":"2026-11-09","high":9,"low":2,"code":2}],
		"attribution":{"text":"credit","url":"https://credit.example/"}}`}}
	f, sources, err := GetForecast(context.Background(), c, 47, 8, "Europe/Zurich", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Hourly) != 1 || *f.Hourly[0].SunshineMinutes != 40 || len(f.Daily) != 1 || f.Attribution.Text != "credit" {
		t.Fatalf("forecast = %+v", f)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %+v", sources)
	}
	// No hours is no forecast.
	c.reply[ForecastTool] = `{"hourly":[],"daily":[]}`
	if _, _, err := GetForecast(context.Background(), c, 47, 8, "Europe/Zurich", 2); err == nil {
		t.Fatal("a forecast without hours was read")
	}
	if _, _, err := GetForecast(context.Background(), &fakeCaller{err: errors.New("down")}, 47, 8, "UTC", 2); err == nil {
		t.Fatal("a failed call was read")
	}
}

func TestLookUpTakesTheBestMatch(t *testing.T) {
	c := &fakeCaller{reply: map[string]string{GeocodeTool: `{"places":[{"name":"Luzern","region":"Luzern","country":"Schweiz",
		"latitude":47.05,"longitude":8.31,"timezone":"Europe/Zurich"},{"name":"Lucerne","country":"USA"}]}`}}
	p, err := Find(context.Background(), c, "Luzern", "", "de")
	if err != nil || p == nil || p.Name != "Luzern" || p.Timezone != "Europe/Zurich" {
		t.Fatalf("place = %+v, %v", p, err)
	}
	c.reply[GeocodeTool] = `{"places":[]}`
	if p, err := Find(context.Background(), c, "Nowhere", "", "de"); p != nil || err != nil {
		t.Fatalf("no match = %+v, %v", p, err)
	}
}

// A country that finds nothing is dropped, not the place.
func TestFindRetriesWithoutTheCountry(t *testing.T) {
	c := &countryCaller{}
	p, err := Find(context.Background(), c, "Luzern", "DE", "de")
	if err != nil || p == nil || p.Name != "Luzern" || p.Timezone != "Europe/Zurich" || len(c.countries) != 2 || c.countries[1] != "" {
		t.Fatalf("place = %+v, %v, asked %v", p, err, c.countries)
	}
	if p, err := Find(context.Background(), &fakeCaller{reply: map[string]string{GeocodeTool: `{"places":[]}`}}, "Nowhere", "", "de"); p != nil || err != nil {
		t.Fatalf("no match = %+v, %v", p, err)
	}
}

// countryCaller finds Luzern only without a country.
type countryCaller struct{ countries []string }

func (c *countryCaller) HasTool(string) bool { return true }

func TestGetWeatherAsksBoth(t *testing.T) {
	forecast := `{"hourly":[{"time":"2026-11-09T09:00","temperature":6,"code":2}],"daily":[]}`
	c := &fakeCaller{reply: map[string]string{ForecastTool: forecast, AirQualityTool: `{"hourly":[{"time":"2026-11-09T09:00","aqi":12}]}`}}
	r, err := GetWeather(context.Background(), c, 47, 8, "Europe/Zurich", 2)
	if err != nil || len(r.Forecast.Hourly) != 1 || r.Air == nil || len(r.Air.Hourly) != 1 || len(r.Sources) != 1 || len(r.AirSources) != 1 {
		t.Fatalf("report = %+v, %v", r, err)
	}
	// A failed air is left out; no air tool, no air asked.
	c.airErr = errors.New("air down")
	if r, err := GetWeather(context.Background(), c, 47, 8, "Europe/Zurich", 2); err != nil || r.Air != nil || len(r.Forecast.Hourly) != 1 {
		t.Fatalf("air down = %+v, %v", r, err)
	}
	only := &fakeCaller{reply: map[string]string{ForecastTool: forecast}}
	if r, err := GetWeather(context.Background(), only, 47, 8, "UTC", 2); err != nil || r.Air != nil || len(only.calls) != 1 {
		t.Fatalf("no air tool = %+v, %v, %v", r, err, only.calls)
	}
	// A failed forecast fails the report.
	if _, err := GetWeather(context.Background(), &fakeCaller{reply: map[string]string{ForecastTool: ""}}, 47, 8, "UTC", 2); err == nil {
		t.Fatal("an unreadable forecast was read")
	}
}

func TestZone(t *testing.T) {
	for _, tc := range []struct{ place, user, want string }{
		{"Europe/Zurich", "America/New_York", "Europe/Zurich"},
		{"", "America/New_York", "America/New_York"},
		{"Nowhere/Else", "", "UTC"},
		{"Local", "Local", "UTC"},
	} {
		if got := Zone(tc.place, tc.user).String(); got != tc.want {
			t.Errorf("Zone(%q, %q) = %s, want %s", tc.place, tc.user, got, tc.want)
		}
	}
}

func (c *countryCaller) CallToolResult(_ context.Context, _ string, args map[string]any) (mcp.CallResult, error) {
	country, _ := args["country"].(string)
	c.countries = append(c.countries, country)
	if country != "" {
		return mcp.CallResult{Text: `{"places":[]}`}, nil
	}
	return mcp.CallResult{Text: `{"places":[{"name":"Luzern","latitude":47.05,"longitude":8.31,"timezone":"Europe/Zurich"}]}`}, nil
}

func TestLanguage(t *testing.T) {
	for in, want := range map[string]string{"": "en", "de-CH": "de", "FR": "fr", "deutsch": "en"} {
		if got := Language(in); got != want {
			t.Errorf("Language(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAirLevels(t *testing.T) {
	for aqi, want := range map[int]string{0: "good", 20: "good", 21: "fair", 40: "fair", 55: "moderate", 80: "poor",
		81: "veryPoor", 100: "veryPoor", 101: "extremelyPoor"} {
		if got := AQILevel(aqi); got != want {
			t.Errorf("aqi %d = %s, want %s", aqi, got, want)
		}
	}
	for _, tc := range []struct {
		kind  string
		value float64
		want  string
	}{
		{"birch", 0.4, ""}, {"birch", 1, "low"}, {"birch", 10.4, "low"}, {"birch", 11, "moderate"},
		{"birch", 299, "high"}, {"birch", 300, "veryHigh"}, {"grass", 150, "veryHigh"}, {"grass", 49, "moderate"},
		{"alder", 250, "veryHigh"}, {"mugwort", 6, "moderate"}, {"ragweed", 40, "veryHigh"}, {"olive", 500, ""},
	} {
		if got := PollenLevel(tc.kind, tc.value); got != tc.want {
			t.Errorf("%s %v = %q, want %q", tc.kind, tc.value, got, tc.want)
		}
	}
}

func TestLocateReadsTheReverseLookup(t *testing.T) {
	c := &fakeCaller{reply: map[string]string{ReverseGeocodeTool: `{"places":[{"name":"Baden","region":"Aargau","country":"Schweiz",
		"countryCode":"CH","latitude":47.47,"longitude":8.31,"timezone":"Europe/Zurich"}],
		"attribution":{"text":"Place names © OpenStreetMap contributors","url":"https://www.openstreetmap.org/copyright"}}`}}
	p, credit, err := Locate(context.Background(), c, 47.48, 8.32, "de")
	if err != nil || p == nil || p.Name != "Baden" || p.Region != "Aargau" || p.Timezone != "Europe/Zurich" || credit.URL == "" {
		t.Fatalf("place = %+v, %+v, %v", p, credit, err)
	}
	c.reply[ReverseGeocodeTool] = `{"places":[]}`
	if p, _, err := Locate(context.Background(), c, 38, -12, "de"); p != nil || err != nil {
		t.Fatalf("the sea = %+v, %v", p, err)
	}
	// The credit becomes a link: only a web address passes.
	for _, bad := range []string{"javascript:alert(1)", "//evil.example/", "ftp://x.example/"} {
		c.reply[ReverseGeocodeTool] = `{"places":[{"name":"Baden"}],"attribution":{"text":"credit","url":"` + bad + `"}}`
		if _, credit, err := Locate(context.Background(), c, 47, 8, "de"); err != nil || credit.URL != "" || credit.Text != "credit" {
			t.Errorf("%s: credit = %+v, %v", bad, credit, err)
		}
	}
	c.reply[ReverseGeocodeTool] = `<html>`
	if _, _, err := Locate(context.Background(), c, 1, 2, "de"); err == nil {
		t.Fatal("an unreadable reply was read")
	}
}
