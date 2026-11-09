package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// The weather contract (docs/weather.md). loom keeps its own copy of the
// names (mcp.WeatherServer, mcp.WeatherForecastTool, mcp.WeatherGeocodeTool);
// the tests hold them together.
const (
	serverName   = "weather"
	forecastTool = "forecast"
	geocodeTool  = "geocode"
	// defaultDays and maxDays bound a forecast's days, today first.
	defaultDays = 2
	maxDays     = 3
	// maxPlaces is how many matches a lookup returns.
	maxPlaces = 5
	// cacheLimit bounds each cache's entries.
	cacheLimit = 4096
)

// Forecast is the forecast tool's answer, as JSON text. Temperatures are °C,
// wind km/h, probabilities %, times local to Timezone without an offset, codes
// WMO weather interpretation codes.
type Forecast struct {
	Latitude    float64     `json:"latitude"`
	Longitude   float64     `json:"longitude"`
	Timezone    string      `json:"timezone"`
	Hourly      []Hour      `json:"hourly"`
	Daily       []Day       `json:"daily"`
	Attribution Attribution `json:"attribution"`
}

type Hour struct {
	Time                     string  `json:"time"`
	Temperature              float64 `json:"temperature"`
	Code                     int     `json:"code"`
	PrecipitationProbability int     `json:"precipitationProbability"`
	WindSpeed                float64 `json:"windSpeed"`
}

type Day struct {
	Date                     string  `json:"date"`
	High                     float64 `json:"high"`
	Low                      float64 `json:"low"`
	Code                     int     `json:"code"`
	PrecipitationProbability int     `json:"precipitationProbability"`
}

// Attribution is the credit the data's licence requires next to it.
type Attribution struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// Place is one match of a place-name lookup.
type Place struct {
	Name        string  `json:"name"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"countryCode,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Timezone    string  `json:"timezone,omitempty"`
}

// weather is the tools' state: the provider and its two caches.
type weather struct {
	provider  openMeteo
	forecasts *cache[Forecast]
	places    *cache[[]Place]
}

// newServer mounts kit's health probes and, behind the bearer token, the
// weather tools at /mcp.
func newServer(cfg config, client *http.Client, version string) http.Handler {
	return newServerAt(cfg, client, version, time.Now)
}

func newServerAt(cfg config, client *http.Client, version string, now func() time.Time) http.Handler {
	w := &weather{
		provider:  openMeteo{http: client, forecastURL: strings.TrimRight(cfg.forecastURL, "/"), geocodingURL: strings.TrimRight(cfg.geocodingURL, "/")},
		forecasts: newCache[Forecast](now, cacheLimit),
		places:    newCache[[]Place](now, cacheLimit),
	}
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(w.mcpServer(version)))
	return sidecar.Handler(cfg.token, api)
}

func (w *weather) mcpServer(version string) mcpserver.Server {
	return mcpserver.Server{Name: serverName, Version: version, Tools: []mcpserver.Tool{{
		Name:  forecastTool,
		Title: "Weather forecast",
		Description: "The weather forecast for a point: hourly temperature (°C), WMO weather code, chance of precipitation (%) and " +
			"wind (km/h), and each day's high, low and code, as JSON. Look the place up with " + geocodeTool + " first when you only have its name.",
		Schema: mcpserver.ObjectSchema(map[string]any{
			"latitude":  map[string]any{"type": "number", "minimum": -90, "maximum": 90},
			"longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
			"timezone":  map[string]any{"type": "string", "description": "IANA zone the times read in; the place's own by default."},
			"days": map[string]any{"type": "integer", "minimum": 1, "maximum": maxDays,
				"description": fmt.Sprintf("Days from today, default %d.", defaultDays)},
		}, "latitude", "longitude"),
		ReadOnly:  true,
		OpenWorld: true,
		Run:       w.runForecast,
	}, {
		Name:  geocodeTool,
		Title: "Find a place",
		Description: fmt.Sprintf("Look a place up by name: up to %d matches with region, country, coordinates and timezone, as JSON, "+
			"best first. Add the country to tell apart places of the same name.", maxPlaces),
		Schema: mcpserver.ObjectSchema(map[string]any{
			"query":    map[string]any{"type": "string", "description": "The place's name or postal code."},
			"country":  map[string]any{"type": "string", "description": "ISO 3166-1 alpha-2 code to search in, e.g. CH."},
			"language": map[string]any{"type": "string", "description": "ISO 639-1 code for the names, default en."},
		}, "query"),
		ReadOnly:  true,
		OpenWorld: true,
		Run:       w.runGeocode,
	}}}
}

var (
	// Zone names, POSIX-style ones such as EST5EDT or Etc/GMT+1 included.
	timezonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)
	countryPattern  = regexp.MustCompile(`^[A-Za-z]{2}$`)
	languagePattern = regexp.MustCompile(`^[a-z]{2}$`)
)

type forecastArgs struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Timezone  string   `json:"timezone"`
	Days      int      `json:"days"`
}

func (w *weather) runForecast(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args forecastArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("invalid arguments: " + err.Error())
	}
	switch {
	case args.Latitude == nil || args.Longitude == nil:
		return mcpserver.ErrorResult("latitude and longitude are required")
	case math.Abs(*args.Latitude) > 90 || math.Abs(*args.Longitude) > 180:
		return mcpserver.ErrorResult("latitude must be within ±90 and longitude within ±180")
	case args.Days < 0 || args.Days > maxDays:
		return mcpserver.ErrorResult(fmt.Sprintf("days must be between 1 and %d", maxDays))
	}
	tz := strings.TrimSpace(args.Timezone)
	if tz == "" {
		tz = "auto"
	} else if !timezonePattern.MatchString(tz) {
		return mcpserver.ErrorResult("timezone must be an IANA zone such as Europe/Zurich")
	}
	days := args.Days
	if days == 0 {
		days = defaultDays
	}
	// About 100 m: every request for one place, a city's centre, is the same
	// key, and the provider's grid is far coarser anyway.
	lat, lon := round3(*args.Latitude), round3(*args.Longitude)
	key := fmt.Sprintf("%.3f,%.3f,%s,%d", lat, lon, tz, days)
	forecast, err := w.forecasts.get(ctx, key, nextHour, func(ctx context.Context) (Forecast, error) {
		return w.provider.forecast(ctx, lat, lon, tz, days)
	})
	if err != nil {
		return mcpserver.ErrorResult("forecast failed: " + err.Error())
	}
	result := jsonResult(forecast)
	// The licence's credit as a source too, so a chat answer cites it even
	// when the model does not repeat it.
	result.Sources = []mcpserver.Source{{URL: forecast.Attribution.URL, Title: forecast.Attribution.Text}}
	return result
}

type geocodeArgs struct {
	Query    string `json:"query"`
	Country  string `json:"country"`
	Language string `json:"language"`
}

func (w *weather) runGeocode(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args geocodeArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("invalid arguments: " + err.Error())
	}
	query := strings.Join(strings.Fields(args.Query), " ")
	country := strings.ToUpper(strings.TrimSpace(args.Country))
	language := strings.ToLower(strings.TrimSpace(args.Language))
	if language == "" {
		language = "en"
	}
	switch {
	case query == "" || len(query) > 100:
		return mcpserver.ErrorResult("query is required, at most 100 characters")
	case country != "" && !countryPattern.MatchString(country):
		return mcpserver.ErrorResult("country must be an ISO 3166-1 alpha-2 code such as CH")
	case !languagePattern.MatchString(language):
		return mcpserver.ErrorResult("language must be an ISO 639-1 code such as de")
	}
	key := strings.ToLower(query) + "|" + country + "|" + language
	places, err := w.places.get(ctx, key, afterGeocodeTTL, func(ctx context.Context) ([]Place, error) {
		return w.provider.geocode(ctx, query, country, language)
	})
	if err != nil {
		return mcpserver.ErrorResult("place lookup failed: " + err.Error())
	}
	return jsonResult(map[string]any{"places": places})
}

func jsonResult(v any) mcpserver.Result {
	text, err := json.Marshal(v)
	if err != nil {
		return mcpserver.ErrorResult("encode result: " + err.Error())
	}
	return mcpserver.Result{Text: string(text)}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
