package mcp

import "strings"

// The weather contract (docs/weather.md) as loom addresses it. The
// loom-weather sidecar serves it; its tests hold these to its names.
const (
	WeatherServer       = "weather"
	WeatherForecastTool = "forecast"
	WeatherGeocodeTool  = "geocode"
)

// WeatherServerConfig is the loom-weather sidecar's endpoint. Best-effort like
// search: a dead sidecar costs the weather, never boot. In a turn its tools
// are offered only on weather questions.
func WeatherServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport:  TransportStreamableHTTP,
		URL:        strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:    map[string]string{"Authorization": "Bearer " + token},
		Categories: []string{"weather"},
		BuiltIn:    true,
	}
}
