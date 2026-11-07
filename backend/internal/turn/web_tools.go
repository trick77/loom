package turn

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/usage"
)

// The web tools loom knows by name. Their names live here, in the policy
// table, and nowhere in dispatch: what is particular to a tool (how its
// result is cited, what it falls back to, how a scheduled run trims it)
// rides on its toolSpec.
const (
	fetchToolName           = "fetch__fetch"
	obscuraNavigateToolName = "obscura__browser_navigate"
	obscuraSnapshotToolName = "obscura__browser_snapshot"
)

// searchToolName is the web search tool as dispatched (docs/search.md),
// derived from the contract's names so a rename there shifts here instead of
// silently zeroing the counter.
var searchToolName = mcp.ExposedToolName(mcp.SearchServer, mcp.SearchTool)

// webToolPolicies are the web tools' policies. fetch and obscura are very
// inexpensive (an HTTP read, a headless page load), so they get a higher cap
// than the default that guards pricier tools. An obscura page load counts per
// navigate; fetch's fallback to the browser navigates outside the tool loop
// and counts itself (fetchBrowserFallback).
var webToolPolicies = []toolSpec{
	{
		name:        fetchToolName,
		capPerRound: cheapToolCallsPerRound,
		concurrent:  true,
		counts:      &usageCounter{"web_fetch", UsageStore.IncWebFetch},
		// The built-in's page cites itself (structuredContent.sources); a
		// replacement that sends none is cited by its url argument.
		relabel:  relabelPageAtURL,
		fallback: fetchBrowserFallback,
		scheduledClamp: func(args map[string]any) {
			if n, ok := args["max_length"].(float64); ok && n > scheduledFetchMaxLength {
				args["max_length"] = scheduledFetchMaxLength
			}
		},
		observe: func(args map[string]any, _ []mcp.Source) ToolObservation {
			return ToolObservation{Tool: "fetch", URL: argURL(args)}
		},
	},
	{
		// Cited by its structured sources, the generic way.
		name:             searchToolName,
		concurrent:       true,
		counts:           &usageCounter{"web_search", UsageStore.IncWebSearch},
		scheduledLimited: true,
		scheduledClamp: func(args map[string]any) {
			if n, ok := args["max_results"].(float64); ok && n > scheduledSearchMaxResults {
				args["max_results"] = scheduledSearchMaxResults
			}
			args["depth"] = "basic"
		},
		observe: func(args map[string]any, sources []mcp.Source) ToolObservation {
			o := ToolObservation{Tool: "search", Query: stringArg(args, "query")}
			for _, s := range sources {
				o.ResultURLs = append(o.ResultURLs, s.URL)
			}
			return o
		},
	},
	{
		name:        obscuraNavigateToolName,
		capPerRound: cheapToolCallsPerRound,
		counts:      &usageCounter{"obscura_fetch", UsageStore.IncObscuraFetch},
		relabel:     relabelBrowserNavigate,
		observe: func(args map[string]any, _ []mcp.Source) ToolObservation {
			return ToolObservation{Tool: "browser", URL: argURL(args)}
		},
	},
	{name: obscuraSnapshotToolName, capPerRound: cheapToolCallsPerRound, relabel: relabelBrowserSnapshot},
}

// serverPolicies hold what applies to every tool of a server that has no
// policy of its own. A search server's other tools (an mcp.json "search" may
// add extract or crawl) cost like its search: a scheduled run counts them too.
var serverPolicies = map[string]*toolSpec{
	mcp.SearchServer: {scheduledLimited: true},
}

// The browser (obscura, a third-party server) sends no sources: navigate
// names the page, snapshot returns its text. These two relabels pair them,
// keeping the navigated page in reg.browserPage for the turn. They always
// run for these tools, so the pairing never goes stale.

// relabelBrowserNavigate registers the navigated page as a source and keeps
// it for the snapshot that follows (the call that returns the text).
func relabelBrowserNavigate(arguments map[string]any, output string, _ []mcp.Source, reg *webSourceRegistry) string {
	reg.browserPage = browserPage{}
	if strings.HasPrefix(output, toolFailedPrefix) {
		// Nothing was delivered: no source, and the next snapshot is not
		// labelled with an earlier page.
		return output
	}
	out := prependURLSource(argURL(arguments), output, reg)
	if idx, ok := reg.add(argURL(arguments)); ok {
		reg.browserPage = browserPage{header: fmt.Sprintf("Source [%d]: %s", idx, strings.TrimSpace(argURL(arguments))), index: idx}
	}
	return out
}

// relabelBrowserSnapshot gives a snapshot the navigated page's marker and
// backfills that source's snippet from the rendered text.
func relabelBrowserSnapshot(_ map[string]any, output string, _ []mcp.Source, reg *webSourceRegistry) string {
	page := reg.browserPage
	if page.header == "" {
		return output
	}
	if pos := page.index - reg.offset - 1; pos >= 0 && pos < len(reg.sources) {
		if src := &reg.sources[pos]; src.Snippet == "" {
			src.Snippet = snippetFromText(output)
		}
	}
	return page.header + "\n\n" + output
}

// browserPage is the page the browser last navigated to in this turn: its
// header and source index, for the snapshot's citation.
type browserPage struct {
	header string
	index  int
}

// fetchBrowserFallback retries a failed fetch in obscura's headless browser
// (navigate, then snapshot the rendered page), when obscura is configured
// and open to this user and the call carried a URL. On success it returns
// the snapshot and true; otherwise false, so the caller surfaces the
// original fetch failure.
func fetchBrowserFallback(ctx context.Context, t *Run, arguments map[string]any, round int, reg *webSourceRegistry) (string, bool) {
	if !t.e.mcp.HasTool(obscuraNavigateToolName) || !t.e.mcp.HasTool(obscuraSnapshotToolName) {
		return "", false
	}
	// The fallback calls obscura directly, so it honours a users allowlist on
	// obscura itself the way an offered call would.
	if !t.e.mcp.ToolAllowed(MCPPrincipal(t.user), obscuraNavigateToolName) {
		return "", false
	}
	url, ok := arguments["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		return "", false
	}
	if _, err := t.e.mcp.CallTool(ctx, obscuraNavigateToolName, map[string]any{"url": url}); err != nil {
		slog.Warn("obscura fallback navigate failed", "url", url, "round", round, "err", err)
		return "", false
	}
	snapshot, err := t.e.mcp.CallTool(ctx, obscuraSnapshotToolName, map[string]any{})
	if err != nil {
		slog.Warn("obscura fallback snapshot failed", "url", url, "round", round, "err", err)
		return "", false
	}
	slog.Info("fetch failed, obscura fallback succeeded", "url", url, "round", round, "result_bytes", len(snapshot))
	usage.Record(t.e.usage, "obscura_fetch", func() error { return t.e.usage.IncObscuraFetch(ctx, t.user.ID) })
	// The requested fetch URL is the source; annotate the rendered snapshot with
	// its [n] marker so the model cites it inline like any other web source.
	if reg == nil {
		return capToolOutput(t.capScheduledOutput(snapshot)), true
	}
	return capToolOutput(prependURLSource(url, t.capScheduledOutput(snapshot), reg)), true
}
