package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// The search contract (docs/search.md). loom keeps its own copy of the names
// (mcp.SearchServer, mcp.SearchTool); the tests hold the two together.
const (
	serverName = "search"
	toolName   = "web"
	// defaultMaxResults and maxMaxResults bound max_results.
	defaultMaxResults = 5
	maxMaxResults     = 10
)

var (
	topics     = []string{"general", "news"}
	timeRanges = []string{"day", "week", "month", "year"}
	depths     = []string{"basic", "advanced"}
)

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, the search tool at /mcp.
func newServer(cfg config, client *http.Client, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(mcpServer(cfg, client, version)))
	return sidecar.Handler(cfg.token, api)
}

// mcpServer is the search server: the web tool when a provider key is set,
// else no tool at all (loom then simply has no web search).
func mcpServer(cfg config, client *http.Client, version string) mcpserver.Server {
	srv := mcpserver.Server{Name: serverName, Version: version}
	if cfg.tavilyKey == "" {
		return srv
	}
	provider := tavily{http: client, baseURL: cfg.tavilyURL, key: cfg.tavilyKey}
	srv.Tools = []mcpserver.Tool{{
		Name:  toolName,
		Title: "Web search",
		Description: "Search the web. Returns the top results, each with its title, URL, publication date when known " +
			"and an extract of the page. Use it for current events, facts that may have changed, and anything you " +
			"are not sure of; fetch a result's URL when its extract is not enough.",
		Schema: mcpserver.ObjectSchema(map[string]any{
			"query": map[string]any{"type": "string", "description": "What to search for, as a search engine query."},
			"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": maxMaxResults,
				"description": fmt.Sprintf("How many results, default %d.", defaultMaxResults)},
			"topic": map[string]any{"type": "string", "enum": topics,
				"description": "news for recent events and reporting, general (the default) for everything else."},
			"time_range": map[string]any{"type": "string", "enum": timeRanges,
				"description": "Only results published within this span back from today."},
			"depth": map[string]any{"type": "string", "enum": depths,
				"description": "advanced searches deeper and returns more relevant extracts, slower; basic (the default) is enough for most questions."},
		}, "query"),
		ReadOnly:  true,
		OpenWorld: true,
		Run: func(ctx context.Context, raw json.RawMessage) mcpserver.Result {
			return runSearch(ctx, provider, raw)
		},
	}}
	return srv
}

type webArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Topic      string `json:"topic"`
	TimeRange  string `json:"time_range"`
	Depth      string `json:"depth"`
}

// runSearch is one call of the web tool: arguments checked against the
// contract, the provider asked, its results as unnumbered text plus sources
// in the same order (loom numbers them).
func runSearch(ctx context.Context, provider tavily, raw json.RawMessage) mcpserver.Result {
	req, err := searchRequest(raw)
	if err != nil {
		return mcpserver.ErrorResult(err.Error())
	}
	results, err := provider.search(ctx, req)
	if err != nil {
		return mcpserver.ErrorResult("web search failed: " + err.Error())
	}
	var text strings.Builder
	sources := []mcpserver.Source{}
	for _, r := range results {
		url := strings.TrimSpace(r.URL)
		if url == "" {
			continue
		}
		title := strings.TrimSpace(r.Title)
		content := strings.TrimSpace(r.Content)
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		fmt.Fprintf(&text, "Title: %s\nURL: %s\n", title, url)
		if date := strings.TrimSpace(r.PublishedDate); date != "" {
			fmt.Fprintf(&text, "Published: %s\n", date)
		}
		fmt.Fprintf(&text, "Content: %s\n", content)
		sources = append(sources, mcpserver.Source{URL: url, Title: title, Snippet: content})
	}
	if len(sources) == 0 {
		// An empty, non-nil list: loom reads it as "no sources", not as a
		// server that sends none.
		return mcpserver.Result{Text: "No results for: " + req.Query, Sources: sources}
	}
	return mcpserver.Result{Text: text.String(), Sources: sources}
}

// searchRequest checks the arguments against the contract. A max_results
// above the cap is lowered to it (the model asked for many, it gets the
// most); anything else out of the contract is an error the model can read.
func searchRequest(raw json.RawMessage) (tavilyRequest, error) {
	var args webArgs
	if err := mcpserver.DecodeArgs(raw, &args); err != nil {
		return tavilyRequest{}, fmt.Errorf("invalid arguments: %w", err)
	}
	req := tavilyRequest{
		Query:       strings.TrimSpace(args.Query),
		MaxResults:  args.MaxResults,
		Topic:       strings.ToLower(strings.TrimSpace(args.Topic)),
		TimeRange:   strings.ToLower(strings.TrimSpace(args.TimeRange)),
		SearchDepth: strings.ToLower(strings.TrimSpace(args.Depth)),
	}
	switch {
	case req.Query == "":
		return tavilyRequest{}, fmt.Errorf("query is required")
	case req.MaxResults < 0:
		return tavilyRequest{}, fmt.Errorf("max_results must be between 1 and %d", maxMaxResults)
	case req.MaxResults == 0:
		req.MaxResults = defaultMaxResults
	case req.MaxResults > maxMaxResults:
		req.MaxResults = maxMaxResults
	}
	for _, f := range []struct {
		name, value string
		allowed     []string
	}{{"topic", req.Topic, topics}, {"time_range", req.TimeRange, timeRanges}, {"depth", req.SearchDepth, depths}} {
		if f.value != "" && !slices.Contains(f.allowed, f.value) {
			return tavilyRequest{}, fmt.Errorf("%s must be one of %s", f.name, strings.Join(f.allowed, ", "))
		}
	}
	return req, nil
}
