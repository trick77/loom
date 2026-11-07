package turn

import (
	"strings"
	"testing"
	"time"
)

func TestRunNowReadsInTheTurnsZone(t *testing.T) {
	fixed := time.Date(2026, time.November, 7, 23, 30, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }
	for zone, want := range map[string]string{
		"Europe/Zurich": "2026-11-08 00:30 CET",
		"":              "2026-11-07 23:30 UTC",
		"Not/AZone":     "2026-11-07 23:30 UTC",
	} {
		run := &Run{timezone: zone, scheduled: &ScheduledRun{Now: clock}}
		if got := run.now().Format("2006-01-02 15:04 MST"); got != want {
			t.Errorf("zone %q: now = %s, want %s", zone, got, want)
		}
	}
}

// A scheduled run knows when it fired, in the schedule's zone, on its first
// run too.
func TestScheduledRunStartedNote(t *testing.T) {
	start := time.Date(2026, time.November, 7, 6, 0, 0, 0, time.UTC)
	run := &Run{timezone: "Europe/Zurich", start: start, scheduled: &ScheduledRun{}}
	if got, want := run.startedNote(), "Run started: Saturday, 2026-11-07 07:00 CET."; got != want {
		t.Fatalf("startedNote = %q, want %q", got, want)
	}
}

func TestDropImages(t *testing.T) {
	cases := map[string]string{
		"a ![poster](https://evil.example/p.png?d=1) b":     "a poster b",
		"![![x](y)](https://evil.example/?d=nested)":        "x",
		"![a [b] c](https://evil.example/?d=balanced)":      "[a [b] c](https://evil.example/?d=balanced)",
		"![ref]\n\n[ref]: https://evil.example/?d=shortcut": "[ref]\n\n[ref]: https://evil.example/?d=shortcut",
		"!![x](https://evil.example/?d=bang)":               "!x",
		"see [the page](https://arena.ch/programm)":         "see [the page](https://arena.ch/programm)",
		// Code blocks are not spared: where one ends is the renderer's call.
		"```bash``` x\n![p](https://evil.example/)": "```bash``` x\np",
		"~~~~\n![a](b)\n~~~\n![c](d)":               "~~~~\na\n~~~\nc",
	}
	for in, want := range cases {
		if got := dropImages(in); got != want {
			t.Errorf("dropImages(%q) = %q, want %q", in, got, want)
		}
	}
}

// An image left in a fenced block never renders, and no prose image remains.
func TestDropImagesLeavesNoProseImage(t *testing.T) {
	for _, in := range []string{"!![", "![![![", "x ![y] ![z][w] ![", "\\![a](b)"} {
		if got := dropImages(in); strings.Contains(got, "![") {
			t.Errorf("dropImages(%q) = %q still holds an image", in, got)
		}
	}
}

func TestDropAnswerImagesCleansReasoning(t *testing.T) {
	out := dropAnswerImages(LoopResult{
		ActivityTrace: []ActivityTraceEvent{reasoningEvent("r1", "think ![x](https://evil.example/?d=1)")},
		Blocks: []ContentBlock{
			{Type: "trace", Events: []ActivityTraceEvent{reasoningEvent("r1", "think ![x](https://evil.example/?d=1)")}},
			{Type: "text", Content: "![y](https://evil.example/?d=2)"},
		},
	})
	if got := out.ActivityTrace[0].Content; got != "think x" {
		t.Fatalf("trace reasoning = %q", got)
	}
	if got := out.Blocks[0].Events[0].Content; got != "think x" {
		t.Fatalf("block reasoning = %q", got)
	}
	if got := out.Blocks[1].Content; got != "y" {
		t.Fatalf("text block = %q", got)
	}
}
