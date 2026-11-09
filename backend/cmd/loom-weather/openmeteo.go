package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// openMeteo is Open-Meteo's forecast and geocoding APIs, the calls this
// sidecar makes. Nothing about Open-Meteo leaves this file but the
// attribution its licence (CC BY 4.0) asks for next to the data: the tools
// speak the weather contract (docs/weather.md).
type openMeteo struct {
	http          *http.Client
	forecastURL   string
	geocodingURL  string
	airQualityURL string
}

// attribution is the credit the free tier's licence requires wherever its
// data is shown.
var attribution = Attribution{Text: "Weather data by Open-Meteo.com", URL: "https://open-meteo.com/"}

// maxProviderBody bounds a reply. The largest, four days of ten hourly
// values, is some 20 KiB: a reply near this is not a forecast.
const maxProviderBody = 1 << 20

// Each request asks at most 10 variables: past that the provider counts one
// request as more than one call against the free tier's daily limit. The
// hours carry sunshine, not the UV index: loom picks sunny hours for going
// out, and the days still carry the UV peak.
const (
	hourlyVariables = "temperature_2m,weather_code,precipitation_probability,precipitation,wind_speed_10m," +
		"wind_gusts_10m,wind_direction_10m,apparent_temperature,relative_humidity_2m,sunshine_duration"
	dailyVariables = "temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max," +
		"precipitation_sum,uv_index_max,sunshine_duration,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant"
)

// forecastPart is one of the two requests a forecast is made of: the hours
// and the days, fetched and kept apart.
type forecastPart struct {
	Latitude  float64
	Longitude float64
	Timezone  string
	// UTCOffset is the place's offset from UTC in seconds.
	UTCOffset int
	Hourly    []Hour
	Daily     []Day
}

func forecastQuery(lat, lon float64, timezone string, days int) url.Values {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("timezone", timezone)
	q.Set("forecast_days", strconv.Itoa(days))
	return q
}

type forecastHeader struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	UTCOffset int     `json:"utc_offset_seconds"`
}

// hourly is the hours from today's midnight at the place on, days days.
func (p openMeteo) hourly(ctx context.Context, lat, lon float64, timezone string, days int) (forecastPart, error) {
	q := forecastQuery(lat, lon, timezone, days)
	q.Set("hourly", hourlyVariables)
	var raw struct {
		forecastHeader
		Hourly struct {
			Time          []string   `json:"time"`
			Temperature   []*float64 `json:"temperature_2m"`
			Code          []*int     `json:"weather_code"`
			Probability   []*int     `json:"precipitation_probability"`
			Precipitation []*float64 `json:"precipitation"`
			Wind          []*float64 `json:"wind_speed_10m"`
			Gusts         []*float64 `json:"wind_gusts_10m"`
			Direction     []*float64 `json:"wind_direction_10m"`
			Apparent      []*float64 `json:"apparent_temperature"`
			Humidity      []*float64 `json:"relative_humidity_2m"`
			Sunshine      []*float64 `json:"sunshine_duration"`
		} `json:"hourly"`
	}
	if err := p.get(ctx, p.forecastURL+"/v1/forecast?"+q.Encode(), &raw); err != nil {
		return forecastPart{}, err
	}
	out := partOf(raw.forecastHeader)
	h := raw.Hourly
	for i, t := range h.Time {
		temp, code := at(h.Temperature, i), at(h.Code, i)
		if temp == nil || code == nil {
			continue
		}
		out.Hourly = append(out.Hourly, Hour{Time: t, Temperature: *temp, Code: *code,
			PrecipitationProbability: at(h.Probability, i), Precipitation: at(h.Precipitation, i),
			WindSpeed: at(h.Wind, i), WindGusts: at(h.Gusts, i), WindDirection: whole(at(h.Direction, i)),
			ApparentTemperature: at(h.Apparent, i), Humidity: whole(at(h.Humidity, i)), SunshineMinutes: minutes(at(h.Sunshine, i))})
	}
	if len(out.Hourly) == 0 {
		return forecastPart{}, fmt.Errorf("weather provider sent no forecast")
	}
	return out, nil
}

// daily is the days from today at the place on, days of them.
func (p openMeteo) daily(ctx context.Context, lat, lon float64, timezone string, days int) (forecastPart, error) {
	q := forecastQuery(lat, lon, timezone, days)
	q.Set("daily", dailyVariables)
	var raw struct {
		forecastHeader
		Daily struct {
			Time          []string   `json:"time"`
			High          []*float64 `json:"temperature_2m_max"`
			Low           []*float64 `json:"temperature_2m_min"`
			Code          []*int     `json:"weather_code"`
			Probability   []*int     `json:"precipitation_probability_max"`
			Precipitation []*float64 `json:"precipitation_sum"`
			UV            []*float64 `json:"uv_index_max"`
			Sunshine      []*float64 `json:"sunshine_duration"`
			Wind          []*float64 `json:"wind_speed_10m_max"`
			Gusts         []*float64 `json:"wind_gusts_10m_max"`
			Direction     []*float64 `json:"wind_direction_10m_dominant"`
		} `json:"daily"`
	}
	if err := p.get(ctx, p.forecastURL+"/v1/forecast?"+q.Encode(), &raw); err != nil {
		return forecastPart{}, err
	}
	out := partOf(raw.forecastHeader)
	d := raw.Daily
	for i, t := range d.Time {
		high, low, code := at(d.High, i), at(d.Low, i), at(d.Code, i)
		if high == nil || low == nil || code == nil {
			continue
		}
		out.Daily = append(out.Daily, Day{Date: t, High: *high, Low: *low, Code: *code,
			PrecipitationProbability: at(d.Probability, i), PrecipitationSum: at(d.Precipitation, i),
			UVIndexMax: at(d.UV, i), SunshineHours: hours(at(d.Sunshine, i)), WindSpeedMax: at(d.Wind, i),
			WindGustsMax: at(d.Gusts, i), WindDirection: whole(at(d.Direction, i))})
	}
	if len(out.Daily) == 0 {
		return forecastPart{}, fmt.Errorf("weather provider sent no daily forecast")
	}
	return out, nil
}

// pollenKinds are the provider's pollen variables, by the contract's names;
// Europe only and only in season, null elsewhere.
var pollenKinds = []string{"alder", "birch", "grass", "mugwort", "olive", "ragweed"}

// airAttribution is the credit the air data's licence asks for: CAMS, the
// models behind it, and the provider.
var airAttribution = Attribution{
	Text: "Air quality by Open-Meteo.com, CAMS data (Copernicus Atmosphere Monitoring Service)",
	URL:  "https://open-meteo.com/en/docs/air-quality-api",
}

// airPart is the air at a place hour by hour, from today's midnight there.
type airPart struct {
	Latitude  float64
	Longitude float64
	Timezone  string
	UTCOffset int
	Hours     []airHour
}

type airHour struct {
	Time string
	AQI  *float64
	// Pollen by pollenKinds' order, grains/m³.
	Pollen []*float64
}

// air is the European air quality index and pollen, days days.
func (p openMeteo) air(ctx context.Context, lat, lon float64, timezone string, days int) (airPart, error) {
	q := forecastQuery(lat, lon, timezone, days)
	variables := []string{"european_aqi"}
	for _, kind := range pollenKinds {
		variables = append(variables, kind+"_pollen")
	}
	q.Set("hourly", strings.Join(variables, ","))
	var raw struct {
		forecastHeader
		Hourly map[string]json.RawMessage `json:"hourly"`
	}
	if err := p.get(ctx, p.airQualityURL+"/v1/air-quality?"+q.Encode(), &raw); err != nil {
		return airPart{}, err
	}
	var times []string
	if err := json.Unmarshal(raw.Hourly["time"], &times); err != nil || len(times) == 0 {
		return airPart{}, fmt.Errorf("weather provider sent no air quality")
	}
	column := func(name string) []*float64 {
		var values []*float64
		_ = json.Unmarshal(raw.Hourly[name], &values) // missing: all unknown
		return values
	}
	aqi := column("european_aqi")
	pollen := make([][]*float64, len(pollenKinds))
	for k, kind := range pollenKinds {
		pollen[k] = column(kind + "_pollen")
	}
	out := airPart{Latitude: raw.Latitude, Longitude: raw.Longitude, Timezone: raw.Timezone, UTCOffset: raw.UTCOffset}
	for i, t := range times {
		h := airHour{Time: t, AQI: at(aqi, i), Pollen: make([]*float64, len(pollenKinds))}
		for k := range pollenKinds {
			h.Pollen[k] = at(pollen[k], i)
		}
		out.Hours = append(out.Hours, h)
	}
	return out, nil
}

func partOf(h forecastHeader) forecastPart {
	return forecastPart{Latitude: h.Latitude, Longitude: h.Longitude, Timezone: h.Timezone, UTCOffset: h.UTCOffset,
		Hourly: []Hour{}, Daily: []Day{}}
}

// whole rounds a value the contract sends as a whole number (degrees,
// percent); the provider's are, but a fraction must not fail the reply.
func whole(v *float64) *int {
	if v == nil {
		return nil
	}
	n := int(math.Round(*v))
	return &n
}

// hours turns the provider's sunshine seconds into hours, to a tenth.
func hours(seconds *float64) *float64 {
	if seconds == nil {
		return nil
	}
	h := math.Round(*seconds/360) / 10
	return &h
}

// minutes turns the provider's sunshine seconds of an hour into minutes.
func minutes(seconds *float64) *int {
	if seconds == nil {
		return nil
	}
	m := int(math.Round(*seconds / 60))
	return &m
}

func (p openMeteo) geocode(ctx context.Context, query, country, language string) ([]Place, error) {
	q := url.Values{}
	q.Set("name", query)
	q.Set("count", strconv.Itoa(maxPlaces))
	q.Set("language", language)
	if country != "" {
		q.Set("countryCode", country)
	}
	var raw struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Timezone    string  `json:"timezone"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Admin1      string  `json:"admin1"`
		} `json:"results"`
	}
	if err := p.get(ctx, p.geocodingURL+"/v1/search?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	places := []Place{}
	for _, r := range raw.Results {
		places = append(places, Place{Name: r.Name, Region: r.Admin1, Country: r.Country, CountryCode: r.CountryCode,
			Latitude: r.Latitude, Longitude: r.Longitude, Timezone: r.Timezone})
	}
	return places, nil
}

func (p openMeteo) get(ctx context.Context, target string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("weather provider unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBody+1))
	if err != nil {
		return fmt.Errorf("weather provider reply unreadable: %w", err)
	}
	if len(body) > maxProviderBody {
		return fmt.Errorf("weather provider reply over %d MiB", maxProviderBody>>20)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather provider answered %d: %.200s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("weather provider reply unreadable: %w", err)
	}
	return nil
}

func at[T any](values []*T, i int) *T {
	if i < len(values) {
		return values[i]
	}
	return nil
}
