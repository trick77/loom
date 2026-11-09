package main

import (
	"net/url"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr         = ":8100"
	defaultForecastURL  = "https://api.open-meteo.com"
	defaultGeocodingURL = "https://geocoding-api.open-meteo.com"
)

type config struct {
	addr         string
	token        string
	forecastURL  string
	geocodingURL string
}

// loadConfig reads WEATHER_* through kit's env reader: trimmed, empty is unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:         r.String("WEATHER_ADDR", defaultAddr),
		token:        r.String("WEATHER_TOKEN", ""),
		forecastURL:  r.String("WEATHER_FORECAST_URL", defaultForecastURL),
		geocodingURL: r.String("WEATHER_GEOCODING_URL", defaultGeocodingURL),
	}
	// Required: the provider's free tier is rate-limited per IP, and whoever
	// calls spends it. loom checks its side with the same rule.
	if msg := sidecar.TokenProblem("WEATHER_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	for name, raw := range map[string]string{"WEATHER_FORECAST_URL": cfg.forecastURL, "WEATHER_GEOCODING_URL": cfg.geocodingURL} {
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.Fail("%s must be an absolute http(s) URL", name)
		}
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
