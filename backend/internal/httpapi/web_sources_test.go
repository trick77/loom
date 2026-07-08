package httpapi

import (
	"strings"
	"testing"
)

func TestDeriveLabel(t *testing.T) {
	cases := map[string]string{
		"truefoundry.com":         "Truefoundry",
		"www.truefoundry.com":     "Truefoundry",
		"modal.com":               "Modal",
		"www.foo.co.uk":           "Foo",
		"sub.a.io":                "A",
		"docs.github.com":         "Github",
		"blog.example.org":        "Example",
		"WWW.Example.COM":         "Example",
		"kubernetes.io":           "Kubernetes",
		"news.ycombinator.com":    "Ycombinator",
		"a.very.deep.sub.foo.com": "Foo",
	}
	for host, want := range cases {
		if got := deriveLabel(host); got != want {
			t.Errorf("deriveLabel(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestNormalizeURLDedupe(t *testing.T) {
	reg := newWebSourceRegistry()
	// Same page, different fragment / trailing slash -> one index.
	i1, ok1 := reg.add("https://modal.com/docs/")
	i2, ok2 := reg.add("https://modal.com/docs#section")
	if !ok1 || !ok2 {
		t.Fatalf("expected both URLs to normalize, got ok1=%v ok2=%v", ok1, ok2)
	}
	if i1 != i2 {
		t.Errorf("expected same index for dedupe, got %d and %d", i1, i2)
	}
	if reg.len() != 1 {
		t.Errorf("expected 1 source after dedupe, got %d", reg.len())
	}
	// A different path -> a new, monotonic index.
	i3, _ := reg.add("https://modal.com/pricing")
	if i3 != 2 {
		t.Errorf("expected second unique URL to be index 2, got %d", i3)
	}
	// Non-http(s) is rejected.
	if _, ok := reg.add("ftp://example.com/file"); ok {
		t.Error("expected ftp URL to be rejected")
	}
	if _, ok := reg.add("not a url"); ok {
		t.Error("expected garbage to be rejected")
	}
}

func TestRelabelTavilyText(t *testing.T) {
	// The shape the Tavily MCP server emits (formatResults).
	raw := strings.Join([]string{
		"Answer: TrueFoundry and Modal are ML platforms.",
		"",
		"Detailed Results:",
		"",
		"Title: TrueFoundry | ML Platform",
		"URL: https://truefoundry.com",
		"Content: TrueFoundry lets teams deploy models on Kubernetes.",
		"",
		"Title: Modal Docs",
		"URL: https://modal.com/docs",
		"Content: Modal runs Python serverlessly.",
	}, "\n")

	reg := newWebSourceRegistry()
	out := relabelTavilyResult(raw, reg)

	if !strings.Contains(out, "[1] Title: TrueFoundry") {
		t.Errorf("expected [1] marker on first result, got:\n%s", out)
	}
	if !strings.Contains(out, "[2] Title: Modal") {
		t.Errorf("expected [2] marker on second result, got:\n%s", out)
	}
	if reg.len() != 2 {
		t.Fatalf("expected 2 registered sources, got %d", reg.len())
	}
	if reg.all()[0].Label != "Truefoundry" || reg.all()[0].Index != 1 {
		t.Errorf("source 1 = %+v, want Truefoundry/1", reg.all()[0])
	}
	if reg.all()[1].Label != "Modal" || reg.all()[1].URL != "https://modal.com/docs" {
		t.Errorf("source 2 = %+v", reg.all()[1])
	}
}

func TestRelabelTavilyJSONFallback(t *testing.T) {
	raw := `{"results":[{"title":"TrueFoundry","url":"https://truefoundry.com"},{"title":"Modal","url":"https://modal.com"}]}`
	reg := newWebSourceRegistry()
	out := relabelTavilyResult(raw, reg)
	if reg.len() != 2 {
		t.Fatalf("expected 2 sources from JSON, got %d", reg.len())
	}
	if !strings.Contains(out, "[1] TrueFoundry — https://truefoundry.com") {
		t.Errorf("expected numbered list from JSON, got:\n%s", out)
	}
}

func TestRelabelTavilyURLSweepFallback(t *testing.T) {
	raw := "Some unstructured blob mentioning https://truefoundry.com and https://modal.com/docs here."
	reg := newWebSourceRegistry()
	out := relabelTavilyResult(raw, reg)
	if reg.len() != 2 {
		t.Fatalf("expected 2 swept sources, got %d", reg.len())
	}
	if !strings.Contains(out, "Web sources (cite with [n]):") {
		t.Errorf("expected appended source list, got:\n%s", out)
	}
}

func TestPrependURLSourceFetch(t *testing.T) {
	reg := newWebSourceRegistry()
	out := prependURLSource("https://modal.com/docs", "The page content.", reg)
	if !strings.HasPrefix(out, "Web source [1]: https://modal.com/docs") {
		t.Errorf("expected fetch header, got:\n%s", out)
	}
	if reg.len() != 1 || reg.all()[0].Label != "Modal" {
		t.Errorf("unexpected registry: %+v", reg.all())
	}
}

func TestRelabelWebToolOutputObscuraNavigateThenSnapshot(t *testing.T) {
	srv := &server{}
	reg := newWebSourceRegistry()
	navArgs := map[string]any{"url": "https://truefoundry.com/pricing"}
	// Navigate registers the source and arms the snapshot header.
	_ = srv.relabelWebToolOutput(obscuraNavigateToolName, navArgs, "navigated ok", reg)
	// Snapshot (no URL of its own) inherits the marker.
	snap := srv.relabelWebToolOutput(obscuraSnapshotToolName, map[string]any{}, "<rendered page>", reg)
	if !strings.HasPrefix(snap, "Web source [1]: https://truefoundry.com/pricing") {
		t.Errorf("expected snapshot to inherit navigate marker, got:\n%s", snap)
	}
	if reg.len() != 1 {
		t.Errorf("expected 1 source, got %d", reg.len())
	}
}

func TestRelabelWebToolOutputNonWebToolIsNoop(t *testing.T) {
	srv := &server{}
	reg := newWebSourceRegistry()
	out := srv.relabelWebToolOutput("conversation_search", map[string]any{}, "digest", reg)
	if out != "digest" {
		t.Errorf("expected non-web tool output unchanged, got %q", out)
	}
	if reg.len() != 0 {
		t.Errorf("expected no sources registered, got %d", reg.len())
	}
}

func TestWebSourceCitations(t *testing.T) {
	sources := []webSource{
		{Index: 1, URL: "https://truefoundry.com", Label: "Truefoundry"},
		{Index: 2, URL: "https://modal.com", Label: "Modal"},
	}
	cits := webSourceCitations(sources)
	if len(cits) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(cits))
	}
	if cits[0].URL != "https://truefoundry.com" || cits[0].Index != 1 || cits[0].Filename != "Truefoundry" {
		t.Errorf("citation 0 = %+v", cits[0])
	}
	if cits[0].DocumentID != "" {
		t.Errorf("web citation should have no DocumentID, got %q", cits[0].DocumentID)
	}
}
