package mcp

import (
	"strings"
	"testing"
)

func TestStravaIsPerUserAndGated(t *testing.T) {
	sc := StravaServerConfig("http://strava:8050/", "tok", []string{"a"})
	if !sc.PerUser || !sc.RestrictUsers || !sc.BuiltIn || sc.URL != "http://strava:8050/mcp" || sc.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("config = %+v", sc)
	}
	if strings.Join(sc.Categories, ",") != "training,health" {
		t.Errorf("categories = %v", sc.Categories)
	}
}
