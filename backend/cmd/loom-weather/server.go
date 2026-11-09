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
	// airQualityTool is optional in the contract: loom shows the air only
	// where a server has it.
	airQualityTool = "air_quality"
	// maxAirDays is how far the provider's European pollen reaches.
	maxAirDays = 4
	// airHours is how many hours of the index an answer lists.
	airHours = 24
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

// AirQuality is the air_quality tool's answer, as JSON text: the European
// air quality index, pollen grains/m³, times local to Timezone.
type AirQuality struct {
	Latitude    float64     `json:"latitude"`
	Longitude   float64     `json:"longitude"`
	Timezone    string      `json:"timezone"`
	Hourly      []AirHour   `json:"hourly"`
	Daily       []AirDay    `json:"daily"`
	Attribution Attribution `json:"attribution"`
}

type AirHour struct {
	Time string `json:"time"`
	AQI  *int   `json:"aqi,omitempty"`
}

type AirDay struct {
	Date   string `json:"date"`
	AQIMax *int   `json:"aqiMax,omitempty"`
	// Pollen is each kind's mean over the day; a kind without values is left
	// out, and a day without any has none.
	Pollen map[string]float64 `json:"pollen,omitempty"`
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
	air      *cache[airPart]
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
		provider: openMeteo{http: client, forecastURL: strings.TrimRight(cfg.forecastURL, "/"),
			geocodingURL: strings.TrimRight(cfg.geocodingURL, "/"), airQualityURL: strings.TrimRight(cfg.airQualityURL, "/")},
		hourly: newCache[forecastPart](now, cacheLimit),
		daily:  newCache[forecastPart](now, cacheLimit),
		air:    newCache[airPart](now, cacheLimit),
		places: newCache[[]Place](now, cacheLimit),
		now:    now,
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
	}, {
		Name:  airQualityTool,
		Title: "Air quality and pollen",
		Description: fmt.Sprintf("The air at a point, as JSON: the European air quality index (0-20 good, 20-40 fair, 40-60 "+
			"moderate, 60-80 poor, 80-100 very poor, above extremely poor) hour by hour for %d hours, and each day's highest "+
			"index and mean pollen (grains/m³) of alder, birch, grass, mugwort, olive and ragweed. Pollen only in Europe and "+
			"in season: a kind left out has none.", airHours),
		Schema: mcpserver.ObjectSchema(map[string]any{
			"latitude":  map[string]any{"type": "number", "minimum": -90, "maximum": 90},
			"longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
			"timezone":  map[string]any{"type": "string", "description": "IANA zone the times read in; the place's own by default."},
			"days": map[string]any{"type": "integer", "minimum": 1, "maximum": maxAirDays,
				"description": fmt.Sprintf("Days from today, default %d.", defaultDays)},
		}, "latitude", "longitude"),
		ReadOnly:  true,
		OpenWorld: true,
		Run:       w.runAirQuality,
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

// point is a call's place, zone and days, checked.
type point struct {
	lat, lon float64
	tz       string
	days     int
	// key is the cache key: about 100 m, so every request for one place, a
	// city's centre, is the same key (the provider's grid is far coarser
	// anyway). It leaves out days: the most is fetched once and cut to each
	// call.
	key string
}

// pointOf reads a call's point; days defaults to defaultDays, up to most.
func pointOf(raw json.RawMessage, most int) (point, *mcpserver.Result) {
	fail := func(msg string) (point, *mcpserver.Result) {
		r := mcpserver.ErrorResult(msg)
		return point{}, &r
	}
	var args forecastArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return fail("invalid arguments: " + err.Error())
	}
	switch {
	case args.Latitude == nil || args.Longitude == nil:
		return fail("latitude and longitude are required")
	case math.Abs(*args.Latitude) > 90 || math.Abs(*args.Longitude) > 180:
		return fail("latitude must be within ±90 and longitude within ±180")
	case args.Days < 0 || args.Days > float64(most) || args.Days != math.Trunc(args.Days):
		return fail(fmt.Sprintf("days must be a whole number between 1 and %d", most))
	}
	tz := strings.TrimSpace(args.Timezone)
	if tz == "" {
		tz = "auto"
	} else if !timezonePattern.MatchString(tz) {
		return fail("timezone must be an IANA zone such as Europe/Zurich")
	}
	p := point{lat: round3(*args.Latitude), lon: round3(*args.Longitude), tz: tz, days: int(args.Days)}
	if p.days == 0 {
		p.days = defaultDays
	}
	p.key = fmt.Sprintf("%.3f,%.3f,%s", p.lat, p.lon, p.tz)
	return p, nil
}

func (w *weather) runForecast(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	p, bad := pointOf(raw, maxDays)
	if bad != nil {
		return *bad
	}
	lat, lon, tz, days, key := p.lat, p.lon, p.tz, p.days, p.key
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

func (w *weather) runAirQuality(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	p, bad := pointOf(raw, maxAirDays)
	if bad != nil {
		return *bad
	}
	part, err := w.air.get(ctx, p.key, nextSixHours, func(ctx context.Context) (airPart, error) {
		return w.provider.air(ctx, p.lat, p.lon, p.tz, maxAirDays)
	})
	if err != nil {
		return mcpserver.ErrorResult("air quality failed: " + err.Error())
	}
	air := airQualityOf(part, p.days, w.now())
	result := jsonResult(air)
	result.Sources = []mcpserver.Source{{URL: air.Attribution.URL, Title: air.Attribution.Text}}
	return result
}

// airQualityOf cuts the kept air to one call's: the hours from the current
// one at the place for airHours, the days from today through days, each with
// its highest index and its pollen as the day's mean, the measure pollen
// load classes are defined on.
func airQualityOf(part airPart, days int, now time.Time) AirQuality {
	local := now.UTC().Add(time.Duration(part.UTCOffset) * time.Second)
	current := local.Truncate(time.Hour).Format("2006-01-02T15:04")
	today := local.Format(time.DateOnly)
	end := local.AddDate(0, 0, days).Format(time.DateOnly)
	out := AirQuality{Latitude: part.Latitude, Longitude: part.Longitude, Timezone: part.Timezone,
		Hourly: []AirHour{}, Daily: []AirDay{}, Attribution: airAttribution}
	type sum struct {
		total float64
		n     int
	}
	var day *AirDay
	var sums []sum
	flush := func() {
		if day == nil {
			return
		}
		for k, s := range sums {
			if s.n > 0 {
				if day.Pollen == nil {
					day.Pollen = map[string]float64{}
				}
				day.Pollen[pollenKinds[k]] = math.Round(s.total/float64(s.n)*10) / 10
			}
		}
		out.Daily = append(out.Daily, *day)
	}
	for _, h := range part.Hours {
		if h.Time >= current && len(out.Hourly) < airHours {
			out.Hourly = append(out.Hourly, AirHour{Time: h.Time, AQI: whole(h.AQI)})
		}
		date := h.Time[:min(10, len(h.Time))]
		if date < today || date >= end {
			continue
		}
		if day == nil || day.Date != date {
			flush()
			day, sums = &AirDay{Date: date}, make([]sum, len(pollenKinds))
		}
		if aqi := whole(h.AQI); aqi != nil && (day.AQIMax == nil || *aqi > *day.AQIMax) {
			day.AQIMax = aqi
		}
		for k, v := range h.Pollen {
			if v != nil {
				sums[k].total += *v
				sums[k].n++
			}
		}
	}
	flush()
	return out
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
