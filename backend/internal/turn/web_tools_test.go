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
	if !strings.HasPrefix(got, "Web source [1]: https://a.example/page") || reg.len() != 1 {
		t.Errorf("by url: %q", got)
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

// What is particular to a web tool rides on its policy; dispatch only reads it.
func TestWebToolPolicies(t *testing.T) {
	e := &Engine{}
	if e.toolPolicy(tavilySearchExposedName).fixedArgs["include_favicon"] != true || !e.toolPolicy(tavilySearchExposedName).scheduledLimited {
		t.Error("tavily: favicon or scheduled limit lost")
	}
	if e.toolPolicy(fetchToolName).fallback == nil || e.toolPolicy(fetchToolName).relabel == nil {
		t.Error("fetch: fallback or url relabel lost")
	}
	for _, name := range []string{fetchToolName, tavilySearchExposedName, obscuraNavigateToolName} {
		if e.toolPolicy(name).observe == nil {
			t.Errorf("%s: no scheduled observation", name)
		}
	}
	args := map[string]any{"max_results": float64(50), "include_raw_content": true}
	e.toolPolicy(tavilySearchExposedName).scheduledClamp(args)
	if args["max_results"] != any(scheduledSearchMaxResults) || args["search_depth"] != "basic" || args["include_raw_content"] != nil {
		t.Errorf("tavily clamp: %v", args)
	}
	o := e.toolPolicy(tavilySearchExposedName).observe(map[string]any{"query": "q"}, []mcp.Source{{URL: "https://r.example"}})
	if o.Tool != "search" || o.Query != "q" || len(o.ResultURLs) != 1 {
		t.Errorf("tavily observe: %+v", o)
	}
	if o := e.toolPolicy(obscuraNavigateToolName).observe(map[string]any{"url": "https://u.example"}, nil); o.Tool != "browser" || o.URL != "https://u.example" {
		t.Errorf("browser observe: %+v", o)
	}
}
