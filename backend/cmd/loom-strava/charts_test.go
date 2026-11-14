package main

import (
	"strings"
	"testing"

	"github.com/trick77/toolkit/mcpserver"
)

func TestChartCalls(t *testing.T) {
	files := []mcpserver.File{{Name: "strava_7_baseline.csv"}, {Name: "strava_7_streams.csv"}, {Name: "strava_7_load.csv"}}
	s := series{hr: []float64{140}, dist: []float64{0}}
	got := chartCalls(7, "Run", files, s, 6)
	for _, want := range []string{
		// Drawn first: the tool loop ends at a round without a tool call.
		"before you answer, draw the charts due with one run_python call, then answer",
		`loomcharts.load_profile("in/strava_7_baseline.csv", "out/load_profile_6w_7.png", weeks=6, lang=L)`,
		`loomcharts.hr_profile("in/strava_7_streams.csv", "out/hr_profile_7.png", lang=L)`,
		`loomcharts.split_variation("in/strava_7_streams.csv", "out/split_variation_7.png", metric="hr", sport="Run", lang=L)`,
		`loomcharts.fitness_fatigue("in/strava_7_load.csv", "out/fitness_fatigue_7.png", lang=L)  (only when asked`,
		"a question about the activity as a whole → every call above but fitness_fatigue",
		"a narrower question → only the one that answers it, or none",
		"A chart this conversation already shows (the same out/ name) is not drawn again unless the user asks for it",
		"In an answer about pain or an injury draw only fitness_fatigue.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}

	// Without the load file, no call and no rule names fitness_fatigue: a call
	// to a missing file fails the whole job. A pain answer then draws none.
	got = chartCalls(7, "Run", files[:2], s, 6)
	if strings.Contains(got, "fitness_fatigue") || !strings.Contains(got, "every call above;") ||
		!strings.Contains(got, "pain or an injury draw none") {
		t.Errorf("no load file:\n%s", got)
	}
	// Only the load file: nothing for a whole-activity question.
	got = chartCalls(7, "Run", files[2:], series{}, 6)
	if !strings.Contains(got, "none for a question about the activity as a whole") {
		t.Errorf("only the load file:\n%s", got)
	}
	if got := chartCalls(7, "Run", nil, s, 6); got != "" {
		t.Errorf("no files: %q", got)
	}
}
