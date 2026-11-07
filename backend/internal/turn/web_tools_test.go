package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/mcp"
)

// A result's own structured sources win over the tool's relabel: the built-in
// fetch cites itself; a replacement that sends none is cited by its url.
func TestRelabelPrefersStructuredSources(t *testing.T) {
	e := &Engine{}
	args := map[string]any{"url": "https://a.example/page"}
	reg := newWebSourceRegistryAfter(0)
	got := e.relabelWebToolOutput(fetchToolName, args, "page text", []mcp.Source{{URL: "https://a.example/page", Snippet: "page text"}}, reg)
	if !strings.HasPrefix(got, "Source [1]: https://a.example/page\n") {
		t.Errorf("structured: %q", got)
	}
	reg = newWebSourceRegistryAfter(0)
	got = e.relabelWebToolOutput(fetchToolName, args, "page text", nil, reg)
	if !strings.HasPrefix(got, "Source [1]: https://a.example/page") || reg.len() != 1 {
		t.Errorf("by url: %q", got)
	}
	// Search is cited by its structured sources, in their order.
	reg = newWebSourceRegistryAfter(0)
	got = e.relabelWebToolOutput(searchToolName, map[string]any{"query": "q"}, "Title: A\nURL: https://a.example\n",
		[]mcp.Source{{URL: "https://a.example", Title: "A"}, {URL: "https://b.example", Title: "B"}}, reg)
	if !strings.HasPrefix(got, "Source [1]: A — https://a.example\nSource [2]: B — https://b.example\n") || reg.len() != 2 {
		t.Errorf("search: %q", got)
	}
	// A tool loom does not know, without sources, is left alone.
	if got := e.relabelWebToolOutput("other__tool", args, "text", nil, newWebSourceRegistryAfter(0)); got != "text" {
		t.Errorf("unknown tool: %q", got)
	}
}

// A titled structured source names its title; an untitled one its URL once.
func TestStructuredSourceHeader(t *testing.T) {
	got := relabelStructuredSources("x", []mcp.Source{{URL: "https://a.example", Title: "A"}, {URL: "https://b.example"}}, newWebSourceRegistryAfter(0))
	if !strings.Contains(got, "Source [1]: A — https://a.example\n") || !strings.Contains(got, "Source [2]: https://b.example\n") {
		t.Errorf("header: %q", got)
	}
}

// Every tool of the search server counts against a scheduled run's paid-search
// limit, not only its search: an mcp.json "search" may add extract or crawl.
func TestSearchServerToolsAllCountForScheduledRuns(t *testing.T) {
	e := &Engine{}
	for _, name := range []string{searchToolName, "search__extract", "search__crawl"} {
		if !e.toolPolicy(name).scheduledLimited {
			t.Errorf("%s not limited", name)
		}
	}
	if e.toolPolicy("other__search").scheduledLimited {
		t.Error("another server's tool limited")
	}
}

// The browser's own relabel runs even when its result carries sources, so
// its navigate/snapshot pairing never goes stale.
func TestBrowserRelabelKeepsPairingWithSources(t *testing.T) {
	e := &Engine{}
	reg := newWebSourceRegistryAfter(0)
	_ = e.relabelWebToolOutput(obscuraNavigateToolName, map[string]any{"url": "https://a.example"}, "ok", nil, reg)
	_ = e.relabelWebToolOutput(obscuraNavigateToolName, map[string]any{"url": "https://b.example"}, "ok", []mcp.Source{{URL: "https://b.example"}}, reg)
	got := e.relabelWebToolOutput(obscuraSnapshotToolName, nil, "page b", nil, reg)
	if !strings.HasPrefix(got, "Source [2]: https://b.example") {
		t.Errorf("snapshot labelled %q, want page b's marker", got)
	}
}

// What is particular to a web tool rides on its policy; dispatch only reads it.
func TestWebToolPolicies(t *testing.T) {
	e := &Engine{}
	// Search cites by its structured sources: no relabel of its own.
	if search := e.toolPolicy(searchToolName); !search.scheduledLimited || !search.concurrent || search.relabel != nil || search.counts == nil || search.counts.label != "web_search" {
		t.Error("search: scheduled limit, concurrency or counter lost, or a relabel crept back")
	}
	if e.toolPolicy(fetchToolName).fallback == nil || e.toolPolicy(fetchToolName).relabel == nil {
		t.Error("fetch: fallback or url relabel lost")
	}
	for _, name := range []string{fetchToolName, searchToolName, obscuraNavigateToolName} {
		if e.toolPolicy(name).observe == nil {
			t.Errorf("%s: no scheduled observation", name)
		}
	}
	args := map[string]any{"max_results": float64(50), "depth": "advanced"}
	e.toolPolicy(searchToolName).scheduledClamp(args)
	if args["max_results"] != any(scheduledSearchMaxResults) || args["depth"] != "basic" {
		t.Errorf("search clamp: %v", args)
	}
	// A count under the cap stays the model's.
	args = map[string]any{"max_results": float64(3)}
	e.toolPolicy(searchToolName).scheduledClamp(args)
	if args["max_results"] != float64(3) {
		t.Errorf("search clamp raised: %v", args)
	}
	o := e.toolPolicy(searchToolName).observe(map[string]any{"query": "q"}, []mcp.Source{{URL: "https://r.example"}})
	if o.Tool != "search" || o.Query != "q" || len(o.ResultURLs) != 1 {
		t.Errorf("search observe: %+v", o)
	}
	if o := e.toolPolicy(obscuraNavigateToolName).observe(map[string]any{"url": "https://u.example"}, nil); o.Tool != "browser" || o.URL != "https://u.example" {
		t.Errorf("browser observe: %+v", o)
	}
}
