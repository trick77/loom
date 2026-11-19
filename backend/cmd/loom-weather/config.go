package main

import (
	"strings"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr          = ":8100"
	defaultForecastURL   = "https://api.open-meteo.com"
	defaultGeocodingURL  = "https://geocoding-api.open-meteo.com"
	defaultAirQualityURL = "https://air-quality-api.open-meteo.com"
	defaultArchiveURL    = "https://archive-api.open-meteo.com"
	defaultReverseURL    = "https://nominatim.openstreetmap.org"
)

type config struct {
	addr          string
	token         string
	forecastURL   string
	geocodingURL  string
	airQualityURL string
	archiveURL    string
	// reverseURL is the map provider naming a point's town; "" (off) drops
	// the reverse lookup.
	reverseURL string
}

// loadConfig reads WEATHER_* through kit's env reader: trimmed, empty is unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:  r.String("WEATHER_ADDR", defaultAddr),
		token: r.String("WEATHER_TOKEN", ""),
	}
	// The provider's free tier is rate-limited per IP and whoever calls spends
	// it, so every tool route needs the token. Without one the sidecar answers
	// its health probe and refuses everything else: compose starts it on every
	// install, set up for the weather or not. loom checks its side alike.
	if msg := sidecar.TokenProblem("WEATHER_TOKEN", cfg.token); cfg.token != "" && msg != "" {
		r.Fail("%s", msg)
	}
	setURL := func(dst *string, name, def string) {
		if u := r.URL(name, def); u != nil {
			*dst = u.String()
		}
	}
	setURL(&cfg.forecastURL, "WEATHER_FORECAST_URL", defaultForecastURL)
	setURL(&cfg.geocodingURL, "WEATHER_GEOCODING_URL", defaultGeocodingURL)
	setURL(&cfg.airQualityURL, "WEATHER_AIR_QUALITY_URL", defaultAirQualityURL)
	setURL(&cfg.archiveURL, "WEATHER_ARCHIVE_URL", defaultArchiveURL)
	if !strings.EqualFold(r.String("WEATHER_REVERSE_URL", ""), "off") {
		setURL(&cfg.reverseURL, "WEATHER_REVERSE_URL", defaultReverseURL)
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
