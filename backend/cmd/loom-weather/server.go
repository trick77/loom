package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/geo"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// The weather contract (docs/weather.md). loom keeps its own copy of the
// names (mcp.WeatherServer, mcp.WeatherForecastTool, mcp.WeatherGeocodeTool, …);
// the tests hold them together.
const (
	serverName   = "weather"
	forecastTool = "forecast"
	geocodeTool  = "geocode"
	// airQualityTool is optional in the contract: loom shows the air only
	// where a server has it.
	airQualityTool = "air_quality"
	// reverseGeocodeTool is optional too: without it loom asks for the
	// place by name only.
	reverseGeocodeTool = "reverse_geocode"
	// maxMatches is the most matches a lookup answers; the reverse lookup
	// weighs that many of a name's for the one nearest the point: a common
	// name (Neustadt) has more namesakes in one country than the default.
	maxMatches = 20
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
	// SunshineMinutes is the hour's sunshine, 0-60.
	SunshineMinutes *int `json:"sunshineMinutes,omitempty"`
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
//
// A day's range is stretched over its kept hours: the days are kept three
// hours, the hours one, and an hour's temperature must never fall outside
// the range given for its day.
func forecastOf(hourly, daily forecastPart, days int, now time.Time) Forecast {
	local := placeClock(now, hourly.Timezone, hourly.UTCOffset)
	current, today := hourStamp(local), local.Format(time.DateOnly)
	hoursEnd, daysEnd := dateAfter(local, min(days, hourlyDays)), dateAfter(local, days)
	out := Forecast{Latitude: hourly.Latitude, Longitude: hourly.Longitude, Timezone: hourly.Timezone,
		Hourly: []Hour{}, Daily: []Day{}, Attribution: attribution}
	type span struct{ low, high float64 }
	spans := map[string]span{}
	for _, h := range hourly.Hourly {
		if date := h.Time[:min(10, len(h.Time))]; date != "" {
			s, ok := spans[date]
			if !ok {
				s = span{h.Temperature, h.Temperature}
			}
			spans[date] = span{min(s.low, h.Temperature), max(s.high, h.Temperature)}
		}
		if h.Time >= current && h.Time < hoursEnd {
			out.Hourly = append(out.Hourly, h)
		}
	}
	for _, d := range daily.Daily {
		if d.Date >= today && d.Date < daysEnd {
			if s, ok := spans[d.Date]; ok {
				d.Low, d.High = min(d.Low, s.low), max(d.High, s.high)
			}
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
	// reverse is nil while the reverse lookup is off; towns keeps its
	// answers by rounded point, townNames the map provider's.
	reverse   *nominatim
	towns     *cache[[]Place]
	townNames *cache[town]
	history   *cache[History]
	now       func() time.Time
}

// newServer mounts kit's health probes and, behind the bearer token, the
// weather tools at /mcp.
func newServer(cfg config, client *http.Client, version string) http.Handler {
	return newServerAt(cfg, client, version, time.Now)
}

func newServerAt(cfg config, client *http.Client, version string, now func() time.Time) http.Handler {
	// Named to the providers, as the map provider's usage policy asks.
	agent := sidecar.UserAgent("loom-weather", version)
	w := &weather{
		provider: openMeteo{http: client, forecastURL: strings.TrimRight(cfg.forecastURL, "/"),
			geocodingURL: strings.TrimRight(cfg.geocodingURL, "/"), airQualityURL: strings.TrimRight(cfg.airQualityURL, "/"),
			archiveURL: strings.TrimRight(cfg.archiveURL, "/"), userAgent: agent},
		history:   newCache[History](now, cacheLimit),
		hourly:    newCache[forecastPart](now, cacheLimit),
		daily:     newCache[forecastPart](now, cacheLimit),
		air:       newCache[airPart](now, cacheLimit),
		places:    newCache[[]Place](now, cacheLimit),
		towns:     newCache[[]Place](now, cacheLimit),
		townNames: newCache[town](now, cacheLimit),
		now:       now,
	}
	if cfg.reverseURL != "" {
		w.reverse = &nominatim{http: client, url: strings.TrimRight(cfg.reverseURL, "/"), userAgent: agent, limit: &limiter{}}
	}
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(w.mcpServer(version)))
	return sidecar.Handler(cfg.token, api)
}

func (w *weather) mcpServer(version string) mcpserver.Server {
	s := w.baseServer(version)
	s.Tools = append(s.Tools, historyToolSpec(w))
	if w.reverse != nil {
		s.Tools = append(s.Tools, mcpserver.Tool{
			Name:  reverseGeocodeTool,
			Title: "Name a point's town",
			Description: "The town a point lies in, as JSON: name, region, country, coordinates and timezone of the town's centre, " +
				"or no place (the sea). The point is rounded to about a kilometre first.",
			Schema: mcpserver.ObjectSchema(map[string]any{
				"latitude":  map[string]any{"type": "number", "minimum": -90, "maximum": 90},
				"longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
				"language":  map[string]any{"type": "string", "description": "ISO 639-1 code for the names, default en."},
			}, "latitude", "longitude"),
			ReadOnly:  true,
			OpenWorld: true,
			Run:       w.runReverseGeocode,
		})
	}
	return s
}

func (w *weather) baseServer(version string) mcpserver.Server {
	return mcpserver.Server{Name: serverName, Version: version, Tools: []mcpserver.Tool{{
		Name:  forecastTool,
		Title: "Weather forecast",
		Description: fmt.Sprintf("The weather forecast for a point, as JSON: hourly temperature and feels-like (°C), WMO weather code, "+
			"chance (%%) and amount (mm) of precipitation, wind and gusts (km/h) with direction, humidity (%%) and minutes of sunshine for the "+
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
			"count": map[string]any{"type": "integer", "minimum": 1, "maximum": maxMatches,
				"description": fmt.Sprintf("How many matches, default %d.", maxPlaces)},
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

// zones keeps the zones placeClock loaded: LoadLocation reads the zone file
// on every call.
var zones sync.Map

// placeClock is now at the place, to be compared with its local times as the
// provider writes them. The clock comes from the zone, so a part kept over a
// clock change still reads right; the offset it was fetched with only stands
// in for a zone this host does not know.
func placeClock(now time.Time, timezone string, utcOffset int) time.Time {
	if loc, ok := zones.Load(timezone); ok {
		return now.In(loc.(*time.Location))
	}
	if loc, err := time.LoadLocation(timezone); timezone != "" && err == nil {
		zones.Store(timezone, loc)
		return now.In(loc)
	}
	return now.UTC().Add(time.Duration(utcOffset) * time.Second)
}

// hourStamp is the hour local falls in, as the provider writes it.
func hourStamp(local time.Time) string { return local.Truncate(time.Hour).Format("2006-01-02T15:04") }

// dateAfter is the date days after local's.
func dateAfter(local time.Time, days int) string {
	return local.AddDate(0, 0, days).Format(time.DateOnly)
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
	// Each part is fetched a day longer than a call may ask: a part kept
	// past the place's midnight still holds every day asked. On a miss both
	// wait together, not one after the other.
	getDaily := func() (forecastPart, error) {
		return w.daily.get(ctx, p.key, nextThreeHours, func(ctx context.Context) (forecastPart, error) {
			return w.provider.daily(ctx, p.lat, p.lon, p.tz, maxDays+1)
		})
	}
	var daily forecastPart
	var dailyErr error
	var wg sync.WaitGroup
	if kept, ok := w.daily.peek(p.key); ok {
		daily = kept
	} else {
		wg.Go(func() { daily, dailyErr = getDaily() })
	}
	hourly, err := w.hourly.get(ctx, p.key, nextHour, func(ctx context.Context) (forecastPart, error) {
		return w.provider.hourly(ctx, p.lat, p.lon, p.tz, hourlyDays+1)
	})
	wg.Wait()
	if err != nil {
		return mcpserver.ErrorResult("forecast failed: " + err.Error())
	}
	if dailyErr != nil {
		// The hours stand on their own: answer them without days rather than
		// not at all. Not kept, so the next call asks for the days again.
		slog.Warn("daily forecast failed, answering the hours only", "err", dailyErr)
		daily = forecastPart{}
	}
	forecast := forecastOf(hourly, daily, p.days, w.now())
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
		// A day more than a call may ask: a part kept past the place's
		// midnight still holds every day asked.
		return w.provider.air(ctx, p.lat, p.lon, p.tz, maxAirDays+1)
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
	local := placeClock(now, part.Timezone, part.UTCOffset)
	current, today, end := hourStamp(local), local.Format(time.DateOnly), dateAfter(local, days)
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

// geocodeArgs reads count as a number: some models send 5.0 for 5.
type geocodeArgs struct {
	Query    string  `json:"query"`
	Country  string  `json:"country"`
	Language string  `json:"language"`
	Count    float64 `json:"count"`
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
	case args.Count < 0 || args.Count > maxMatches || args.Count != math.Trunc(args.Count):
		return mcpserver.ErrorResult(fmt.Sprintf("count must be a whole number between 1 and %d", maxMatches))
	}
	count := int(args.Count)
	if count == 0 {
		count = maxPlaces
	}
	places, err := w.lookup(ctx, query, country, language, count)
	if err != nil {
		return mcpserver.ErrorResult("place lookup failed: " + err.Error())
	}
	return jsonResult(map[string]any{"places": places})
}

// lookup is the weather search's first count matches for a name, kept.
func (w *weather) lookup(ctx context.Context, query, country, language string, count int) ([]Place, error) {
	key := fmt.Sprintf("%s|%s|%s|%d", strings.ToLower(query), country, language, count)
	return w.places.get(ctx, key, afterGeocodeTTL, func(ctx context.Context) ([]Place, error) {
		return w.provider.geocode(ctx, query, country, language, count)
	})
}

// reverseAnswer is the reverse_geocode tool's answer: at most one place.
type reverseAnswer struct {
	Places      []Place     `json:"places"`
	Attribution Attribution `json:"attribution"`
}

type reverseArgs struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Language  string   `json:"language"`
}

// runReverseGeocode names the town at a point. The map provider names it;
// the weather search's match nearest the point is the answer, so the place
// is the one a lookup by name finds, with its zone. The point goes out
// rounded to two decimals (about a kilometre): the town is all that is
// asked, and every point within it is one cache key.
func (w *weather) runReverseGeocode(ctx context.Context, raw json.RawMessage) mcpserver.Result {
	var args reverseArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return mcpserver.ErrorResult("invalid arguments: " + err.Error())
	}
	language := strings.ToLower(strings.TrimSpace(args.Language))
	if language == "" {
		language = "en"
	}
	switch {
	case args.Latitude == nil || args.Longitude == nil:
		return mcpserver.ErrorResult("latitude and longitude are required")
	case math.Abs(*args.Latitude) > 90 || math.Abs(*args.Longitude) > 180:
		return mcpserver.ErrorResult("latitude must be within ±90 and longitude within ±180")
	case !languagePattern.MatchString(language):
		return mcpserver.ErrorResult("language must be an ISO 639-1 code such as de")
	}
	lat, lon := geo.Coarse(*args.Latitude), geo.Coarse(*args.Longitude)
	key := fmt.Sprintf("%.2f,%.2f|%s", lat, lon, language)
	places, err := w.towns.get(ctx, key, afterGeocodeTTL, func(ctx context.Context) ([]Place, error) {
		// The map provider's answer is kept apart: a weather search that
		// fails after it must not spend another map request on the retry.
		t, err := w.townNames.get(ctx, key, afterGeocodeTTL, func(ctx context.Context) (town, error) {
			return w.reverse.reverse(ctx, lat, lon, language)
		})
		if err != nil {
			return nil, err
		}
		var lookupErr error
		for _, name := range t.names {
			matches, err := w.lookup(ctx, name, t.countryCode, language, maxMatches)
			if err != nil {
				// A wider name may still answer.
				lookupErr = err
				continue
			}
			if i := geo.Nearest(lat, lon, len(matches), func(i int) (float64, float64) {
				return matches[i].Latitude, matches[i].Longitude
			}); i >= 0 {
				return []Place{matches[i]}, nil
			}
		}
		// No place for a failed search is not kept: the next call asks again.
		if lookupErr != nil {
			return nil, lookupErr
		}
		return []Place{}, nil
	})
	if err != nil {
		return mcpserver.ErrorResult("reverse lookup failed: " + err.Error())
	}
	return jsonResult(reverseAnswer{Places: places, Attribution: reverseAttribution})
}

func jsonResult(v any) mcpserver.Result {
	text, err := json.Marshal(v)
	if err != nil {
		return mcpserver.ErrorResult("encode result: " + err.Error())
	}
	return mcpserver.Result{Text: string(text)}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
