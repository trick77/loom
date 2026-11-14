package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/trick77/toolkit/mcpserver"
)

// chartFirst and chartFirstEnd frame a tool's one chart, drawn before the
// answer like the analysis' (chartCalls) unless this conversation shows it
// already: the out name carries the arguments, so another sport, window or
// unit is a new chart.
const (
	chartFirst = "chart: before you answer, draw it with run_python, this code exactly, L set to \"de\" when you " +
		"answer in German, else left \"en\": import loomcharts; L = \"en\"; "
	chartFirstEnd = "Then answer; the chart shows below your answer: refer to it, never link or embed it. " +
		chartShown + "\n"
	// chartShown: a follow-up question redrew every chart; an explicit ask or
	// new numbers (a new run in the window) still draw it.
	chartShown = "A chart this conversation already shows (the same out/ name) is not drawn again unless the user " +
		"asks for it or this result's numbers differ from the ones it was drawn from: refer to it above."
)

// chartCalls is the run_python code an analysis offers the model, one call
// per chart whose input file is attached: a call naming a file that is not
// there fails the whole job (loomcharts reads it at once). Output names carry
// the activity id: two analyses in one thread never overwrite each other's
// images, and the model can tell a chart it already drew.
//
// Drawn before the answer: the tool loop ends at a round without a tool call,
// so "answer, then draw" lost the charts or, after an empty last round, wrote
// the answer twice; the UI shows images below the answer (artifactsLast). The
// user, on what they saw: a follow-up question redrew all four (each call
// returns the code again: which to draw is the question's); fitness and
// fatigue every time was noise ("unless a) user asks for it b) it's relevant
// for an ask"). L is the one thing the model sets: the chart labels follow
// the answer's language.
func chartCalls(id int64, sport string, files []mcpserver.File, s series, weeks int) string {
	has := func(name string) bool {
		return slices.ContainsFunc(files, func(f mcpserver.File) bool { return f.Name == name })
	}
	in := func(kind string) string { return fmt.Sprintf(`"in/strava_%d_%s.csv"`, id, kind) }
	out := func(chart string) string { return fmt.Sprintf(`"out/%s_%d.png"`, chart, id) }
	type chart struct{ call, when string }
	var whole []chart
	if has(fmt.Sprintf("strava_%d_baseline.csv", id)) {
		// The window in the name: rerun with other weeks, it is another chart.
		whole = append(whole, chart{fmt.Sprintf("loomcharts.load_profile(%s, %s, weeks=%d, lang=L)", in("baseline"),
			out(fmt.Sprintf("load_profile_%dw", weeks)), weeks), "how it compares with the usual"})
	}
	if has(fmt.Sprintf("strava_%d_streams.csv", id)) {
		if s.hr != nil {
			whole = append(whole, chart{fmt.Sprintf("loomcharts.hr_profile(%s, %s, lang=L)", in("streams"), out("hr_profile")),
				"heart rate, intensity, how hard it felt"})
		}
		if s.dist != nil {
			metric := "hr"
			if s.watts != nil {
				metric = "watts"
			}
			whole = append(whole, chart{fmt.Sprintf("loomcharts.split_variation(%s, %s, metric=%q, sport=%q, lang=L)",
				in("streams"), out("split_variation"), metric, sport), "pacing, splits"})
		}
	}
	var fitness *chart
	if has(fmt.Sprintf("strava_%d_load.csv", id)) {
		fitness = &chart{fmt.Sprintf("loomcharts.fitness_fatigue(%s, %s, lang=L)", in("load"), out("fitness_fatigue")),
			"only when asked about fitness, fatigue, form or training load, or for pain, an injury, tiredness or overtraining"}
	}
	if len(whole) == 0 && fitness == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("charts: before you answer, draw the charts due with one run_python call, then answer (they show " +
		"below your answer). The code: import loomcharts; L = \"en\" (\"de\" when you answer in German); then the " +
		"calls, each exactly as here:\n")
	for _, c := range whole {
		fmt.Fprintf(&b, "- %s  (%s)\n", c.call, c.when)
	}
	if fitness != nil {
		fmt.Fprintf(&b, "- %s  (%s)\n", fitness.call, fitness.when)
	}
	switch {
	case len(whole) == 0:
		b.WriteString("Which are due: none for a question about the activity as a whole")
	case fitness != nil:
		b.WriteString("Which are due: a question about the activity as a whole → every call above but fitness_fatigue")
	default:
		b.WriteString("Which are due: a question about the activity as a whole → every call above")
	}
	b.WriteString("; a narrower question → only the one that answers it, or none. " + chartShown)
	// Always said: without it a pain question reads as one about the activity.
	if fitness != nil {
		b.WriteString(" In an answer about pain or an injury draw only fitness_fatigue.")
	} else {
		b.WriteString(" In an answer about pain or an injury draw none.")
	}
	b.WriteString(" Refer to the charts, never link or embed them.\n")
	return b.String()
}
