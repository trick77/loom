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
	maxDays     = 7
	// hourlyDays is how many days the hours cover at most: further out only
	// the days are worth their tokens.
	hourlyDays = 3
	// maxPlaces is how many matches a lookup returns.
	maxPlaces = 5
	// cacheLimit bounds each cache's entries.
	cacheLimit = 4096
)

// Forecast is the forecast tool's answer, as JSON text. Temperatures are °C,
// wind km/h, directions degrees (where it blows from), precipitation mm,
// probabilities and humidity %, times local to Timezone without an offset,
// codes WMO weather interpretation codes. A value the provider has not is
// left out, never sent as 0.
type Forecast struct {
	Latitude    float64     `json:"latitude"`
	Longitude   float64     `json:"longitude"`
	Timezone    string      `json:"timezone"`
	Hourly      []Hour      `json:"hourly"`
	Daily       []Day       `json:"daily"`
	Attribution Attribution `json:"attribution"`
}

type Hour struct {
	Time                     string   `json:"time"`
	Temperature              float64  `json:"temperature"`
	Code                     int      `json:"code"`
	PrecipitationProbability *int     `json:"precipitationProbability,omitempty"`
	Precipitation            *float64 `json:"precipitation,omitempty"`
	WindSpeed                *float64 `json:"windSpeed,omitempty"`
	WindGusts                *float64 `json:"windGusts,omitempty"`
	WindDirection            *int     `json:"windDirection,omitempty"`
	ApparentTemperature      *float64 `json:"apparentTemperature,omitempty"`
	Humidity                 *int     `json:"humidity,omitempty"`
	UVIndex                  *float64 `json:"uvIndex,omitempty"`
}

type Day struct {
	Date                     string   `json:"date"`
	High                     float64  `json:"high"`
	Low                      float64  `json:"low"`
	Code                     int      `json:"code"`
	PrecipitationProbability *int     `json:"precipitationProbability,omitempty"`
	PrecipitationSum         *float64 `json:"precipitationSum,omitempty"`
	UVIndexMax               *float64 `json:"uvIndexMax,omitempty"`
	SunshineHours            *float64 `json:"sunshineHours,omitempty"`
	WindSpeedMax             *float64 `json:"windSpeedMax,omitempty"`
	WindGustsMax             *float64 `json:"windGustsMax,omitempty"`
	// WindDirection is the day's dominant one.
	WindDirection *int `json:"windDirection,omitempty"`
}

// forecastOf cuts the kept hours and days to one call's: hours from the
// current one at the place (earlier ones only cost the model tokens) through
// at most hourlyDays days, days from today through days. The kept parts hold
// more; a part kept over midnight starts a day early, dropped here.
func forecastOf(hourly, daily forecastPart, days int, now time.Time) Forecast {
	local := now.UTC().Add(time.Duration(hourly.UTCOffset) * time.Second)
	current := local.Truncate(time.Hour).Format("2006-01-02T15:04")
	today := local.Format(time.DateOnly)
	hoursEnd := local.AddDate(0, 0, min(days, hourlyDays)).Format(time.DateOnly)
	daysEnd := local.AddDate(0, 0, days).Format(time.DateOnly)
	out := Forecast{Latitude: hourly.Latitude, Longitude: hourly.Longitude, Timezone: hourly.Timezone,
		Hourly: []Hour{}, Daily: []Day{}, Attribution: attribution}
	for _, h := range hourly.Hourly {
		if h.Time >= current && h.Time < hoursEnd {
			out.Hourly = append(out.Hourly, h)
		}
	}
	for _, d := range daily.Daily {
		if d.Date >= today && d.Date < daysEnd {
			out.Daily = append(out.Daily, d)
		}
	}
	return out
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

// weather is the tools' state: the provider and its caches. The hours and
// the days of a forecast are kept apart, each as long as it stays current.
type weather struct {
	provider openMeteo
	hourly   *cache[forecastPart]
	daily    *cache[forecastPart]
	places   *cache[[]Place]
	now      func() time.Time
}

// newServer mounts kit's health probes and, behind the bearer token, the
// weather tools at /mcp.
func newServer(cfg config, client *http.Client, version string) http.Handler {
	return newServerAt(cfg, client, version, time.Now)
}

func newServerAt(cfg config, client *http.Client, version string, now func() time.Time) http.Handler {
	w := &weather{
		provider: openMeteo{http: client, forecastURL: strings.TrimRight(cfg.forecastURL, "/"), geocodingURL: strings.TrimRight(cfg.geocodingURL, "/")},
		hourly:   newCache[forecastPart](now, cacheLimit),
		daily:    newCache[forecastPart](now, cacheLimit),
		places:   newCache[[]Place](now, cacheLimit),
		now:      now,
	}
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(w.mcpServer(version)))
	return sidecar.Handler(cfg.token, api)
}

func (w *weather) mcpServer(version string) mcpserver.Server {
	return mcpserver.Server{Name: serverName, Version: version, Tools: []mcpserver.Tool{{
		Name:  forecastTool,
		Title: "Weather forecast",
		Description: fmt.Sprintf("The weather forecast for a point, as JSON: hourly temperature and feels-like (°C), WMO weather code, "+
			"chance (%%) and amount (mm) of precipitation, wind and gusts (km/h) with direction, humidity (%%) and UV index for the "+
			"first %d days at most; each day's high, low, code, chance and sum of precipitation, UV index, sunshine hours and wind. "+
			"Look the place up with %s first when you only have its name.", hourlyDays, geocodeTool),
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

// forecastArgs reads days as a number: some models send 2.0 for 2.
type forecastArgs struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Timezone  string   `json:"timezone"`
	Days      float64  `json:"days"`
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
	case args.Days < 0 || args.Days > maxDays || args.Days != math.Trunc(args.Days):
		return mcpserver.ErrorResult(fmt.Sprintf("days must be a whole number between 1 and %d", maxDays))
	}
	tz := strings.TrimSpace(args.Timezone)
	if tz == "" {
		tz = "auto"
	} else if !timezonePattern.MatchString(tz) {
		return mcpserver.ErrorResult("timezone must be an IANA zone such as Europe/Zurich")
	}
	days := int(args.Days)
	if days == 0 {
		days = defaultDays
	}
	// About 100 m: every request for one place, a city's centre, is the same
	// key, and the provider's grid is far coarser anyway. The key leaves out
	// days: the most is fetched once and cut to each call.
	lat, lon := round3(*args.Latitude), round3(*args.Longitude)
	key := fmt.Sprintf("%.3f,%.3f,%s", lat, lon, tz)
	// On a miss both wait together, not one after the other.
	var daily forecastPart
	var dailyErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		daily, dailyErr = w.daily.get(ctx, key, nextThreeHours, func(ctx context.Context) (forecastPart, error) {
			return w.provider.daily(ctx, lat, lon, tz, maxDays)
		})
	}()
	hourly, err := w.hourly.get(ctx, key, nextHour, func(ctx context.Context) (forecastPart, error) {
		return w.provider.hourly(ctx, lat, lon, tz, hourlyDays)
	})
	<-done
	if err == nil {
		err = dailyErr
	}
	if err != nil {
		return mcpserver.ErrorResult("forecast failed: " + err.Error())
	}
	forecast := forecastOf(hourly, daily, days, w.now())
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
