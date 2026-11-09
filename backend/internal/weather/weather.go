// Package weather is the weather contract (docs/weather.md) as loom reads it:
// the forecast, the air and place lookups asked of the weather server, and
// loom's own readings of them (air bands, pollen classes, the windows to go
// out in).
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/mcp"
)

// The weather server's tools as a turn and the start page call them.
var (
	ForecastTool   = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherForecastTool)
	GeocodeTool    = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherGeocodeTool)
	AirQualityTool = mcp.ExposedToolName(mcp.WeatherServer, mcp.WeatherAirQualityTool)
)

// Caller is the MCP tool service the weather is asked through.
type Caller interface {
	CallToolResult(ctx context.Context, name string, args map[string]any) (mcp.CallResult, error)
	HasTool(name string) bool
}

// Report is a point's forecast and, where the server has it, its air.
type Report struct {
	Forecast Forecast
	// Sources are the forecast's credit, AirSources the air's, to cite.
	Sources []mcp.Source
	// Air is nil when the server has none or it failed.
	Air        *Air
	AirSources []mcp.Source
}

// airDays is how far the air is asked: today and tomorrow, all a reader
// looks at.
const airDays = 2

// GetWeather asks the forecast and the air together: on a cache miss each
// is a provider round trip, so a reader waits on the slower, not the sum. The
// air is called off when the forecast fails; an air that fails is left out.
func GetWeather(ctx context.Context, c Caller, lat, lon float64, zone string, days int) (Report, error) {
	airCtx, cancelAir := context.WithCancel(ctx)
	defer cancelAir()
	var out Report
	var wg sync.WaitGroup
	if c.HasTool(AirQualityTool) {
		wg.Go(func() {
			a, sources, err := GetAir(airCtx, c, lat, lon, zone, airDays)
			if err != nil {
				if airCtx.Err() == nil {
					slog.Warn("air quality failed", "err", err)
				}
				return
			}
			out.Air, out.AirSources = &a, sources
		})
	}
	f, sources, err := GetForecast(ctx, c, lat, lon, zone, days)
	if err != nil {
		cancelAir()
	}
	wg.Wait()
	if err != nil {
		return Report{}, err
	}
	out.Forecast, out.Sources = f, sources
	return out, nil
}

// Forecast is the forecast tool's answer: beyond time, temperature and code
// every value may be missing.
type Forecast struct {
	Hourly      []Hour      `json:"hourly"`
	Daily       []Day       `json:"daily"`
	Attribution Attribution `json:"attribution"`
}

// Hour is one forecast hour, its time local to the place.
type Hour struct {
	Time                     string   `json:"time"`
	Temperature              float64  `json:"temperature"`
	Code                     int      `json:"code"`
	PrecipitationProbability *int     `json:"precipitationProbability"`
	Precipitation            *float64 `json:"precipitation"`
	WindSpeed                *float64 `json:"windSpeed"`
	WindGusts                *float64 `json:"windGusts"`
	WindDirection            *int     `json:"windDirection"`
	ApparentTemperature      *float64 `json:"apparentTemperature"`
	Humidity                 *int     `json:"humidity"`
	SunshineMinutes          *int     `json:"sunshineMinutes"`
}

// Day is one forecast day.
type Day struct {
	Date                     string   `json:"date"`
	High                     float64  `json:"high"`
	Low                      float64  `json:"low"`
	Code                     int      `json:"code"`
	PrecipitationProbability *int     `json:"precipitationProbability"`
	PrecipitationSum         *float64 `json:"precipitationSum"`
	UVIndexMax               *float64 `json:"uvIndexMax"`
	SunshineHours            *float64 `json:"sunshineHours"`
	WindSpeedMax             *float64 `json:"windSpeedMax"`
	WindGustsMax             *float64 `json:"windGustsMax"`
	WindDirection            *int     `json:"windDirection"`
}

// Air is the air_quality tool's answer. The index is read as a number:
// another server may send fractions.
type Air struct {
	Hourly      []AirHour   `json:"hourly"`
	Daily       []AirDay    `json:"daily"`
	Attribution Attribution `json:"attribution"`
}

// AirHour is one hour's air quality index.
type AirHour struct {
	Time string   `json:"time"`
	AQI  *float64 `json:"aqi"`
}

// AirDay is a day's highest index and mean pollen (grains/m³) by kind.
type AirDay struct {
	Date   string             `json:"date"`
	AQIMax *float64           `json:"aqiMax"`
	Pollen map[string]float64 `json:"pollen"`
}

// Attribution is the credit the data's licence asks for next to it.
type Attribution struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// Place is a geocode match: a town's centre and its zone.
type Place struct {
	Name      string  `json:"name"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

// GetForecast asks for the forecast at a point, days days from today, its
// times read in zone. The sources are the data's credit, to cite.
func GetForecast(ctx context.Context, c Caller, lat, lon float64, zone string, days int) (Forecast, []mcp.Source, error) {
	res, err := c.CallToolResult(ctx, ForecastTool, map[string]any{
		"latitude": lat, "longitude": lon, "timezone": zone, "days": days,
	})
	if err != nil {
		return Forecast{}, nil, fmt.Errorf("forecast: %w", err)
	}
	var f Forecast
	if err := json.Unmarshal([]byte(res.Text), &f); err != nil {
		return Forecast{}, nil, fmt.Errorf("forecast unreadable: %w", err)
	}
	if len(f.Hourly) == 0 {
		return Forecast{}, nil, errors.New("forecast has no hours")
	}
	return f, res.Sources, nil
}

// GetAir asks for the air at a point, days days from today. The sources are
// the data's credit, to cite.
func GetAir(ctx context.Context, c Caller, lat, lon float64, zone string, days int) (Air, []mcp.Source, error) {
	res, err := c.CallToolResult(ctx, AirQualityTool, map[string]any{
		"latitude": lat, "longitude": lon, "timezone": zone, "days": days,
	})
	if err != nil {
		return Air{}, nil, fmt.Errorf("air quality: %w", err)
	}
	var a Air
	if err := json.Unmarshal([]byte(res.Text), &a); err != nil {
		return Air{}, nil, fmt.Errorf("air quality unreadable: %w", err)
	}
	return a, res.Sources, nil
}

// Find is the best match for a place's name, as a location.Place. A country
// (ISO 3166-1 alpha-2, often the model's guess) narrows the lookup; when it
// finds nothing there, the name alone is looked up. nil when none matches.
func Find(ctx context.Context, c Caller, query, country, language string) (*location.Place, error) {
	p, err := LookUp(ctx, c, query, country, language)
	if err == nil && p == nil && country != "" {
		p, err = LookUp(ctx, c, query, "", language)
	}
	if p == nil || err != nil {
		return nil, err
	}
	return &location.Place{Name: p.Name, Region: p.Region, Country: p.Country, Latitude: p.Latitude,
		Longitude: p.Longitude, Timezone: p.Timezone}, nil
}

// LookUp is the best match for a place's name (country, ISO 3166-1 alpha-2,
// narrows it when given), names in language; nil when there is none.
func LookUp(ctx context.Context, c Caller, query, country, language string) (*Place, error) {
	args := map[string]any{"query": query, "language": language}
	if country != "" {
		args["country"] = country
	}
	res, err := c.CallToolResult(ctx, GeocodeTool, args)
	if err != nil {
		return nil, fmt.Errorf("look up %q: %w", query, err)
	}
	var reply struct {
		Places []Place `json:"places"`
	}
	if err := json.Unmarshal([]byte(res.Text), &reply); err != nil {
		return nil, fmt.Errorf("look up %q: unreadable reply: %w", query, err)
	}
	if len(reply.Places) == 0 {
		return nil, nil
	}
	p := reply.Places[0]
	return &p, nil
}

// Zone is the zone a place's times read in: its own, else the user's, else
// UTC. The host's local zone is never one: it is no place's.
func Zone(place, user string) *time.Location {
	for _, name := range []string{place, user} {
		if loc, err := time.LoadLocation(name); name != "" && err == nil && loc != time.Local {
			return loc
		}
	}
	return time.UTC
}

// Language is the lookup language for a user's pinned answer language: its
// base, else English.
func Language(responseLanguage string) string {
	lang, _, _ := strings.Cut(strings.ToLower(responseLanguage), "-")
	if len(lang) != 2 {
		return "en"
	}
	return lang
}

// AQILevel is the European air quality index's band (EEA): good to 20, fair
// to 40, moderate to 60, poor to 80, very poor to 100, extremely poor above.
func AQILevel(aqi int) string {
	switch {
	case aqi <= 20:
		return "good"
	case aqi <= 40:
		return "fair"
	case aqi <= 60:
		return "moderate"
	case aqi <= 80:
		return "poor"
	case aqi <= 100:
		return "veryPoor"
	}
	return "extremelyPoor"
}

// PollenOrder is the kinds loom classes, in the season's order; olive has no
// class here (it barely flies north of the Alps).
var PollenOrder = []string{"alder", "birch", "grass", "mugwort", "ragweed"}

// pollenClasses are where a kind's moderate, high and very high loads start,
// in grains/m³ as a daily mean; low is from 1. MeteoSwiss, "Threshold values
// for pollen load classes of allergenic pollen types" (2024-01-10).
var pollenClasses = map[string][3]float64{
	"alder":   {11, 70, 250},
	"birch":   {11, 70, 300},
	"grass":   {20, 50, 150},
	"mugwort": {6, 15, 50},
	"ragweed": {6, 11, 40},
}

// PollenLevel is a kind's load class for a day's mean; "" when below low or
// the kind has no classes.
func PollenLevel(kind string, mean float64) string {
	classes, ok := pollenClasses[kind]
	v := math.Round(mean)
	switch {
	case !ok || v < 1:
		return ""
	case v >= classes[2]:
		return "veryHigh"
	case v >= classes[1]:
		return "high"
	case v >= classes[0]:
		return "moderate"
	}
	return "low"
}
