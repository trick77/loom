package turn

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
	reg := newWebSourceRegistryAfter(0)
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

// The web_sources SSE snapshot is re-sent after every tool round rather than only
// when the source count grows, because a second tool can enrich an already-known
// URL in place. This pins the reason: the citation payload changes while len() does
// not, so a length-gated emit would leave the sidebar showing degraded data.
func TestWebSourceCitationsReflectBackfillWithoutLengthChange(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	// First seen as a bare fetch: no title, no favicon.
	reg.addDetailed("https://truefoundry.com/blog", "", "", "")
	before := webSourceCitations(reg.all())
	lenBefore := reg.len()

	// A search later returns the same page with full detail.
	reg.addDetailed("https://truefoundry.com/blog", "Serving LLMs", "A guide.", "https://truefoundry.com/fav.ico")

	if reg.len() != lenBefore {
		t.Fatalf("reg.len() = %d, want unchanged %d — the premise of this test is that backfill does not grow the registry", reg.len(), lenBefore)
	}
	after := webSourceCitations(reg.all())
	if before[0].Title != "" {
		t.Fatalf("precondition failed: title was already set: %+v", before[0])
	}
	if after[0].Title != "Serving LLMs" || after[0].Favicon == "" {
		t.Errorf("backfilled citation = %+v, want title and favicon populated", after[0])
	}
}

func TestPrependURLSourceFetch(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	out := prependURLSource("https://modal.com/docs", "The page content.", reg)
	if !strings.HasPrefix(out, "Source [1]: https://modal.com/docs") {
		t.Errorf("expected fetch header, got:\n%s", out)
	}
	if reg.len() != 1 || reg.all()[0].Label != "Modal" {
		t.Errorf("unexpected registry: %+v", reg.all())
	}
}

func TestRelabelWebToolOutputObscuraNavigateThenSnapshot(t *testing.T) {
	srv := &Engine{}
	reg := newWebSourceRegistryAfter(0)
	navArgs := map[string]any{"url": "https://truefoundry.com/pricing"}
	// Navigate registers the source and arms the snapshot header.
	_ = srv.relabelWebToolOutput(obscuraNavigateToolName, navArgs, "navigated ok", nil, reg)
	// Snapshot (no URL of its own) inherits the marker.
	snap := srv.relabelWebToolOutput(obscuraSnapshotToolName, map[string]any{}, "<rendered page>", nil, reg)
	if !strings.HasPrefix(snap, "Source [1]: https://truefoundry.com/pricing") {
		t.Errorf("expected snapshot to inherit navigate marker, got:\n%s", snap)
	}
	if reg.len() != 1 {
		t.Errorf("expected 1 source, got %d", reg.len())
	}
}

func TestRelabelWebToolOutputNonWebToolIsNoop(t *testing.T) {
	srv := &Engine{}
	reg := newWebSourceRegistryAfter(0)
	out := srv.relabelWebToolOutput("conversation_search", map[string]any{}, "digest", nil, reg)
	if out != "digest" {
		t.Errorf("expected non-web tool output unchanged, got %q", out)
	}
	if reg.len() != 0 {
		t.Errorf("expected no sources registered, got %d", reg.len())
	}
}

func TestWebSourceCitations(t *testing.T) {
	sources := []webSource{
		{Index: 1, URL: "https://truefoundry.com", Label: "Truefoundry", Title: "TF", Snippet: "deploys models", Favicon: "https://x/f.png"},
		{Index: 2, URL: "https://modal.com", Label: "Modal"},
	}
	cits := webSourceCitations(sources)
	if len(cits) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(cits))
	}
	if cits[0].URL != "https://truefoundry.com" || cits[0].Index != 1 || cits[0].Filename != "Truefoundry" {
		t.Errorf("citation 0 = %+v", cits[0])
	}
	if cits[0].Title != "TF" || cits[0].Snippet != "deploys models" || cits[0].Favicon != "https://x/f.png" {
		t.Errorf("citation 0 missing sidebar fields: %+v", cits[0])
	}
	if cits[0].DocumentID != "" {
		t.Errorf("web citation should have no DocumentID, got %q", cits[0].DocumentID)
	}
}

func TestFetchSourceCapturesSnippet(t *testing.T) {
	srv := &Engine{}
	reg := newWebSourceRegistryAfter(0)
	page := "  Modal is a serverless   platform\nfor running Python.  "
	out := srv.relabelWebToolOutput(fetchToolName, map[string]any{"url": "https://modal.com/docs"}, page, nil, reg)
	if !strings.HasPrefix(out, "Source [1]: https://modal.com/docs") {
		t.Errorf("expected fetch header, got:\n%s", out)
	}
	if got := reg.all()[0].Snippet; got != "Modal is a serverless platform for running Python." {
		t.Errorf("snippet = %q (whitespace should be collapsed)", got)
	}
}

func TestParseToolArgumentsNullIsWritableMap(t *testing.T) {
	// A literal JSON null must yield a non-nil, writable map (a scheduled
	// run's clamp writes into it), not a nil map that panics on assignment.
	for _, raw := range []string{"null", "", "  "} {
		args, err := parseToolArguments(raw)
		if err != nil {
			t.Fatalf("parseToolArguments(%q) error: %v", raw, err)
		}
		if args == nil {
			t.Fatalf("parseToolArguments(%q) returned nil map", raw)
		}
		args["depth"] = "basic" // must not panic
	}
}

func TestAddDetailedBackfillsEmptyFields(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	// First seen with no detail (e.g. a bare fetch), then again with details (a search).
	reg.addDetailed("https://modal.com/docs", "", "", "")
	reg.addDetailed("https://modal.com/docs#frag", "Modal Docs", "Run Python serverlessly", "https://modal.com/fav.ico")
	if reg.len() != 1 {
		t.Fatalf("expected dedupe to 1 source, got %d", reg.len())
	}
	src := reg.all()[0]
	if src.Title != "Modal Docs" || src.Snippet != "Run Python serverlessly" || src.Favicon != "https://modal.com/fav.ico" {
		t.Errorf("expected backfill, got %+v", src)
	}
}

// A navigate that failed delivered no page: it must not register a source, and
// the snapshot that follows must not inherit a label pointing at it. Before,
// the header armed by an earlier successful navigate was left in place, so the
// next snapshot was labelled with the previous page's URL.
func TestRelabelWebToolOutputFailedNavigateDisarmsSnapshotLabel(t *testing.T) {
	srv := &Engine{}
	reg := newWebSourceRegistryAfter(0)
	_ = srv.relabelWebToolOutput(obscuraNavigateToolName, map[string]any{"url": "https://first.example/"}, "navigated ok", nil, reg)
	_ = srv.relabelWebToolOutput(obscuraSnapshotToolName, map[string]any{}, "<first page>", nil, reg)

	_ = srv.relabelWebToolOutput(obscuraNavigateToolName, map[string]any{"url": "https://second.example/"}, toolFailedPrefix+": timeout", nil, reg)
	snap := srv.relabelWebToolOutput(obscuraSnapshotToolName, map[string]any{}, "<whatever the browser shows now>", nil, reg)

	if strings.Contains(snap, "first.example") || strings.Contains(snap, "second.example") {
		t.Fatalf("snapshot after a failed navigate carries a source label:\n%s", snap)
	}
	if reg.len() != 1 {
		t.Fatalf("sources = %d, want only the successful navigate", reg.len())
	}
}
