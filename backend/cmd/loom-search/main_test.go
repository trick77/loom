package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

func vars(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{"SEARCH_TOKEN": testToken}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.tavilyURL != defaultTavilyURL || cfg.tavilyKey != "" {
		t.Fatalf("defaults %+v", cfg)
	}
	cfg, err = loadConfig(vars(map[string]string{"SEARCH_TOKEN": testToken, "SEARCH_TAVILY_API_KEY": " tvly-key ", "SEARCH_ADDR": ":9000"}))
	if err != nil || cfg.tavilyKey != "tvly-key" || cfg.addr != ":9000" {
		t.Fatalf("overrides %+v, %v", cfg, err)
	}
	for name, values := range map[string]map[string]string{
		"no token":       {},
		"short token":    {"SEARCH_TOKEN": "short"},
		"token space":    {"SEARCH_TOKEN": testToken + " x"},
		"bad tavily url": {"SEARCH_TOKEN": testToken, "SEARCH_TAVILY_URL": "api.tavily.com"},
	} {
		if _, err := loadConfig(vars(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestServerNeedsTheToken(t *testing.T) {
	h := newServer(config{token: testToken, tavilyKey: "k"}, http.DefaultClient, "test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
}

// fakeTavily answers POST /search with results, or with status when set,
// and keeps the request it got.
type fakeTavily struct {
	status  int
	results []tavilyResult
	got     map[string]any
	auth    string
}

func (f *fakeTavily) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		f.auth = r.Header.Get("Authorization")
		f.got = map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&f.got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if f.status != 0 {
			http.Error(w, `{"detail":{"error":"out of credits"}}`, f.status)
			return
		}
		_ = json.NewEncoder(w).Encode(tavilyResponse{Results: f.results})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// loomService is loom's own MCP client on a sidecar in front of fake.
func loomService(t *testing.T, cfg config) *mcp.Service {
	t.Helper()
	cfg.token = testToken
	srv := httptest.NewServer(newServer(cfg, http.DefaultClient, "test"))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.SearchServer: mcp.SearchServerConfig(srv.URL, testToken),
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

var webTool = mcp.ExposedToolName(mcp.SearchServer, mcp.SearchTool)

// loom, through its own MCP client, gets the results as text and as sources
// in the same order: the contract end to end.
func TestLoomGetsTheResults(t *testing.T) {
	fake := &fakeTavily{results: []tavilyResult{
		{Title: "Glazing guide", URL: "https://example.com/glazing", Content: "Balcony glazing needs a permit.", PublishedDate: "2026-10-01"},
		{Title: "No URL", Content: "dropped"},
		{Title: "Forum", URL: "https://forum.example.org/t/1", Content: "Ask the landlord first."},
	}}
	svc := loomService(t, config{tavilyKey: "tvly-key", tavilyURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), webTool, map[string]any{
		"query": "balcony glazing", "max_results": 50, "topic": "News", "time_range": "month", "depth": "advanced",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.auth != "Bearer tvly-key" {
		t.Fatalf("provider auth %q", fake.auth)
	}
	want := map[string]any{"query": "balcony glazing", "max_results": float64(maxMaxResults), "topic": "news", "time_range": "month", "search_depth": "advanced"}
	for k, v := range want {
		if fake.got[k] != v {
			t.Fatalf("provider got %v, want %s=%v", fake.got, k, v)
		}
	}
	if len(got.Sources) != 2 || got.Sources[0].URL != "https://example.com/glazing" || got.Sources[0].Title != "Glazing guide" ||
		got.Sources[0].Snippet != "Balcony glazing needs a permit." || got.Sources[0].Label != "" || got.Sources[1].URL != "https://forum.example.org/t/1" {
		t.Fatalf("sources %+v", got.Sources)
	}
	for _, want := range []string{"Title: Glazing guide\nURL: https://example.com/glazing\nPublished: 2026-10-01\nContent: Balcony glazing needs a permit.", "URL: https://forum.example.org/t/1"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("text %q lacks %q", got.Text, want)
		}
	}
	if strings.Contains(got.Text, "dropped") || strings.Contains(got.Text, "[1]") {
		t.Fatalf("text %q", got.Text)
	}
}

func TestDefaultsAndNoResults(t *testing.T) {
	fake := &fakeTavily{}
	svc := loomService(t, config{tavilyKey: "k", tavilyURL: fake.server(t).URL})
	got, err := svc.CallToolResult(context.Background(), webTool, map[string]any{"query": " nothing "})
	if err != nil {
		t.Fatal(err)
	}
	if fake.got["max_results"] != float64(defaultMaxResults) || fake.got["query"] != "nothing" {
		t.Fatalf("provider got %v", fake.got)
	}
	for _, unset := range []string{"topic", "time_range", "search_depth"} {
		if _, ok := fake.got[unset]; ok {
			t.Fatalf("provider got %s: %v", unset, fake.got)
		}
	}
	if len(got.Sources) != 0 || !strings.Contains(got.Text, "No results") {
		t.Fatalf("result %+v", got)
	}
}

// A bad argument or a failing provider is an error result the model reads,
// never a crash.
func TestErrorsAreToolErrors(t *testing.T) {
	fake := &fakeTavily{status: 432}
	svc := loomService(t, config{tavilyKey: "k", tavilyURL: fake.server(t).URL})
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"provider":     {map[string]any{"query": "x"}, "432"},
		"no query":     {map[string]any{"query": "  "}, "query is required"},
		"negative":     {map[string]any{"query": "x", "max_results": -1}, "max_results"},
		"string count": {map[string]any{"query": "x", "max_results": "5"}, "invalid arguments"},
		"unknown":      {map[string]any{"query": "x", "search_depth": "basic"}, "invalid arguments"},
		"bad topic":    {map[string]any{"query": "x", "topic": "sports"}, "topic must be one of"},
		"bad range":    {map[string]any{"query": "x", "time_range": "decade"}, "time_range must be one of"},
		"bad depth":    {map[string]any{"query": "x", "depth": "deep"}, "depth must be one of"},
	} {
		got, err := svc.CallToolResult(context.Background(), webTool, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %+v, %v; want error with %q", name, got, err, tc.want)
		}
	}
}

func TestUnreachableProvider(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	svc := loomService(t, config{tavilyKey: "k", tavilyURL: dead.URL})
	if _, err := svc.CallToolResult(context.Background(), webTool, map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("err %v", err)
	}
}

func TestUnreadableReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }))
	t.Cleanup(srv.Close)
	svc := loomService(t, config{tavilyKey: "k", tavilyURL: srv.URL})
	if _, err := svc.CallToolResult(context.Background(), webTool, map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("err %v", err)
	}
}

func TestOversizedReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"content":"` + strings.Repeat("x", maxTavilyBody) + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	svc := loomService(t, config{tavilyKey: "k", tavilyURL: srv.URL})
	if _, err := svc.CallToolResult(context.Background(), webTool, map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "fewer results") {
		t.Fatalf("err %v", err)
	}
}

// Without a provider key the sidecar answers but serves no tool: web search
// is off, loom boots as usual.
func TestNoKeyNoTool(t *testing.T) {
	svc := loomService(t, config{tavilyURL: defaultTavilyURL})
	if svc.HasTool(webTool) || len(svc.Tools()) != 0 {
		t.Fatalf("tools %+v", svc.Tools())
	}
}

// loom addresses the contract by its own copy of the names: they must match
// the server's.
func TestContractNamesMatchLoom(t *testing.T) {
	if mcp.SearchServer != serverName || mcp.SearchTool != toolName {
		t.Fatal("loom's search contract names drifted from loom-search's")
	}
}
