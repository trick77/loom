package turn

import (
	"strings"
	"testing"
)

func TestDropImages(t *testing.T) {
	cases := map[string]string{
		"a ![poster](https://evil.example/p.png?d=1) b":                             "a poster b",
		"![![x](y)](https://evil.example/?d=nested)":                                "x",
		"![a [b] c](https://evil.example/?d=balanced)":                              "[a [b] c](https://evil.example/?d=balanced)",
		"![ref]\n\n[ref]: https://evil.example/?d=shortcut":                         "[ref]\n\n[ref]: https://evil.example/?d=shortcut",
		"!![x](https://evil.example/?d=bang)":                                       "!x",
		"see [the page](https://arena.ch/programm)":                                 "see [the page](https://arena.ch/programm)",
		"```md\n![logo](https://x.example/l.png)\n```\n![p](https://evil.example/)": "```md\n![logo](https://x.example/l.png)\n```\np",
		"~~~~\n![a](b)\n~~~\n![c](d)":                                               "~~~~\n![a](b)\n~~~\n![c](d)",
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
