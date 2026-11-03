package mcp

import (
	"strings"

	"github.com/trick77/loom/internal/classifier"
)

// StravaServerConfig is the loom-strava sidecar: each listed user's own
// Strava activities behind that user's own connection (PerUser), offered in
// training and health turns only (an empty users list serves nobody). Loom's
// own sidecar: its instructions are trusted.
func StravaServerConfig(baseURL, token string, users []string) ServerConfig {
	return ServerConfig{
		Transport:         TransportStreamableHTTP,
		URL:               strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:           map[string]string{"Authorization": "Bearer " + token},
		RestrictUsers:     true,
		Users:             users,
		PerUser:           true,
		TrustInstructions: true,
		BuiltIn:           true,
		Categories:        []string{string(classifier.Training), string(classifier.Health)},
	}
}
