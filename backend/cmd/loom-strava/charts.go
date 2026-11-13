package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/trick77/toolkit/mcpserver"
)

// chartCalls is the run_python code every analysis has the model run
// before it answers (the user wants the charts each time): one call per
// chart whose input file is attached. A call naming a file that is not
// there fails the whole job (loomcharts reads it at once), so the code
// names only what this result carries. Output names carry the activity id:
// two analyses in one thread never overwrite each other's images. L is the
// one thing the model sets: the chart labels follow the answer's language.
func chartCalls(id int64, sport string, files []mcpserver.File, s series, weeks int) string {
	has := func(name string) bool {
		return slices.ContainsFunc(files, func(f mcpserver.File) bool { return f.Name == name })
	}
	in := func(kind string) string { return fmt.Sprintf(`"in/strava_%d_%s.csv"`, id, kind) }
	out := func(chart string) string { return fmt.Sprintf(`"out/%s_%d.png"`, chart, id) }
	var calls []string
	if has(fmt.Sprintf("strava_%d_baseline.csv", id)) {
		calls = append(calls, fmt.Sprintf("loomcharts.load_profile(%s, %s, weeks=%d, lang=L)", in("baseline"), out("load_profile"), weeks))
	}
	if has(fmt.Sprintf("strava_%d_load.csv", id)) {
		calls = append(calls, fmt.Sprintf("loomcharts.fitness_fatigue(%s, %s, lang=L)", in("load"), out("fitness_fatigue")))
	}
	if has(fmt.Sprintf("strava_%d_streams.csv", id)) {
		if s.hr != nil {
			calls = append(calls, fmt.Sprintf("loomcharts.hr_profile(%s, %s, lang=L)", in("streams"), out("hr_profile")))
		}
		if s.dist != nil {
			metric := "hr"
			if s.watts != nil {
				metric = "watts"
			}
			calls = append(calls, fmt.Sprintf("loomcharts.split_variation(%s, %s, metric=%q, sport=%q, lang=L)", in("streams"),
				out("split_variation"), metric, sport))
		}
	}
	if len(calls) == 0 {
		return ""
	}
	return "charts: before you answer, draw them with one run_python call, this code exactly, L set to \"de\" when you answer " +
		"in German, else left \"en\": import loomcharts; L = \"en\"; " + strings.Join(calls, "; ") +
		"\nThen answer; the charts are shown to the user: refer to them, never link or embed them. In an answer about pain " +
		"or an injury draw only fitness_fatigue.\n"
}
