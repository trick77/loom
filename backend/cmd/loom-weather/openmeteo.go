package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	http         *http.Client
	forecastURL  string
	geocodingURL string
}

// attribution is the credit the free tier's licence requires wherever its
// data is shown.
var attribution = Attribution{Text: "Weather data by Open-Meteo.com", URL: "https://open-meteo.com/"}

// maxProviderBody bounds a reply: three days of hourly values, never megabytes.
const maxProviderBody = 1 << 20

func (p openMeteo) forecast(ctx context.Context, lat, lon float64, timezone string, days int) (Forecast, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("timezone", timezone)
	q.Set("forecast_days", strconv.Itoa(days))
	q.Set("hourly", "temperature_2m,weather_code,precipitation_probability,wind_speed_10m")
	q.Set("daily", "temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max")
	var raw struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timezone  string  `json:"timezone"`
		UTCOffset int     `json:"utc_offset_seconds"`
		Hourly    struct {
			Time          []string   `json:"time"`
			Temperature   []*float64 `json:"temperature_2m"`
			Code          []*int     `json:"weather_code"`
			Precipitation []*int     `json:"precipitation_probability"`
			Wind          []*float64 `json:"wind_speed_10m"`
		} `json:"hourly"`
		Daily struct {
			Time          []string   `json:"time"`
			High          []*float64 `json:"temperature_2m_max"`
			Low           []*float64 `json:"temperature_2m_min"`
			Code          []*int     `json:"weather_code"`
			Precipitation []*int     `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := p.get(ctx, p.forecastURL+"/v1/forecast?"+q.Encode(), &raw); err != nil {
		return Forecast{}, err
	}
	out := Forecast{Latitude: raw.Latitude, Longitude: raw.Longitude, Timezone: raw.Timezone, Hourly: []Hour{}, Daily: []Day{},
		Attribution: attribution, utcOffset: raw.UTCOffset}
	h := raw.Hourly
	for i, t := range h.Time {
		temp, code := at(h.Temperature, i), at(h.Code, i)
		if temp == nil || code == nil {
			continue
		}
		out.Hourly = append(out.Hourly, Hour{Time: t, Temperature: *temp, Code: *code,
			PrecipitationProbability: at(h.Precipitation, i), WindSpeed: at(h.Wind, i)})
	}
	d := raw.Daily
	for i, t := range d.Time {
		high, low, code := at(d.High, i), at(d.Low, i), at(d.Code, i)
		if high == nil || low == nil || code == nil {
			continue
		}
		out.Daily = append(out.Daily, Day{Date: t, High: *high, Low: *low, Code: *code,
			PrecipitationProbability: at(d.Precipitation, i)})
	}
	if len(out.Hourly) == 0 {
		return Forecast{}, fmt.Errorf("weather provider sent no forecast")
	}
	return out, nil
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
