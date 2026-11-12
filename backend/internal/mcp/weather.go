package mcp

import "strings"

// The weather contract (docs/weather.md) as loom addresses it. The
// loom-weather sidecar serves it; its tests hold these to its names.
const (
	WeatherServer       = "weather"
	WeatherForecastTool = "forecast"
	WeatherGeocodeTool  = "geocode"
	// WeatherAirQualityTool is optional: a replacement server without it
	// costs the start page its air card only.
	WeatherAirQualityTool = "air_quality"
	// WeatherReverseGeocodeTool is optional: without it the start page asks
	// for the place by name only.
	WeatherReverseGeocodeTool = "reverse_geocode"
)

// WeatherServerConfig is the loom-weather sidecar's endpoint. Best-effort like
// search: a dead sidecar costs the weather, never boot. In a turn its tools
// are offered only on weather questions, so a continued thread of another
// category is re-classified to catch a turn that drifts to the weather
// (CoversGated).
func WeatherServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport:  TransportStreamableHTTP,
		URL:        strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:    map[string]string{"Authorization": "Bearer " + token},
		Categories: ContractCategories[WeatherServer],
		BuiltIn:    true,
		ReadOnly:   true,
	}
}
