package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// mustToolConfig runs toolConfigForConfig and returns the union of required +
// file servers (what loom actually exposes), failing the test on error so the
// many built-in-server cases stay terse.
func mustToolConfig(t *testing.T, cfg config.Config) mcp.Config {
	t.Helper()
	got, err := toolConfigForConfig(cfg, nil)
	if err != nil {
		t.Fatalf("toolConfigForConfig() error = %v", err)
	}
	return got.union()
}

func TestResponseLogDirForConfigOnlyEnablesDevMode(t *testing.T) {
	cfg := config.Config{AuthMode: config.AuthModeOIDC, ChatLogDir: "logs/llm-responses"}
	if got := responseLogDirForConfig(cfg); got != "" {
		t.Fatalf("responseLogDirForConfig(OIDC) = %q, want empty", got)
	}

	cfg.AuthMode = config.AuthModeDev
	if got := responseLogDirForConfig(cfg); got != "logs/llm-responses" {
		t.Fatalf("responseLogDirForConfig(dev) = %q, want logs/llm-responses", got)
	}
}

func TestChatClientConfigFromConfig(t *testing.T) {
	cfg := config.Config{
		ChatEnabled:             true,
		ChatMaxCompletionTokens: 4096,
		ChatTimeout:             90 * time.Second,
		ChatIdleTimeout:         60 * time.Second,
		ChatLogDir:              "logs/llm-responses",
		AuthMode:                config.AuthModeDev,
	}

	got := chatClientConfigFromConfig(cfg)
	// The endpoint is llmwire's (LLMWIRE_ZAI_*), never copied through config.
	if got.BaseURL != "" || got.APIKey != "" {
		t.Fatalf("BaseURL/APIKey = %q/%q, want empty so llmwire reads its own variables", got.BaseURL, got.APIKey)
	}
	if got.MaxCompletionTokens != cfg.ChatMaxCompletionTokens {
		t.Fatalf("MaxCompletionTokens = %d, want %d", got.MaxCompletionTokens, cfg.ChatMaxCompletionTokens)
	}
	if got.Timeout != cfg.ChatTimeout {
		t.Fatalf("Timeout = %s, want %s", got.Timeout, cfg.ChatTimeout)
	}
	if got.IdleTimeout != cfg.ChatIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", got.IdleTimeout, cfg.ChatIdleTimeout)
	}
	if got.ResponseLogDir != cfg.ChatLogDir {
		t.Fatalf("ResponseLogDir = %q, want %q", got.ResponseLogDir, cfg.ChatLogDir)
	}
}

func TestToolConfigForConfigWiresPDFExtractorIntoFetch(t *testing.T) {
	// A default Tika URL alone must not enable it: Tika is only boot-checked
	// when document RAG runs, which is what passes an extractor in.
	if mustToolConfig(t, config.Config{TikaURL: "http://tika:9998"}).Servers["fetch"].PDFExtractor != nil {
		t.Fatal("no extractor passed must leave fetch without one")
	}
	extract := func(context.Context, []byte) (string, error) { return "", nil }
	got, err := toolConfigForConfig(config.Config{}, extract)
	if err != nil {
		t.Fatalf("toolConfigForConfig() error = %v", err)
	}
	if got.Required.Servers["fetch"].PDFExtractor == nil {
		t.Fatal("the passed extractor must reach the fetch server config")
	}
}

// The search sidecar is a best-effort built-in: a dead one costs web search,
// never boot.
func TestToolConfigForConfigAddsSearchSidecar(t *testing.T) {
	tc, err := toolConfigForConfig(config.Config{SearchURL: "http://search:8030", SearchToken: strings.Repeat("t", 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := tc.FileServers.Servers[mcp.SearchServer]
	if !ok || !sc.BuiltIn || sc.URL != "http://search:8030/mcp" || sc.Headers["Authorization"] != "Bearer "+strings.Repeat("t", 32) {
		t.Fatalf("search = %+v (present %v)", sc, ok)
	}
	if _, required := tc.Required.Servers[mcp.SearchServer]; required {
		t.Fatal("search sidecar must not be required")
	}
	if tc.Required.Servers["fetch"].Transport != mcp.TransportInProcess {
		t.Fatalf("fetch config = %#v, want in-process transport", tc.Required.Servers["fetch"])
	}
	if _, ok := mustToolConfig(t, config.Config{}).Servers[mcp.SearchServer]; ok {
		t.Fatal("search registered without its URL")
	}
}

// The weather sidecar is a best-effort built-in, its tools offered on weather
// questions only.
func TestToolConfigForConfigAddsWeatherSidecar(t *testing.T) {
	tc, err := toolConfigForConfig(config.Config{WeatherURL: "http://weather:8100", WeatherToken: strings.Repeat("t", 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := tc.FileServers.Servers[mcp.WeatherServer]
	if !ok || !sc.BuiltIn || sc.URL != "http://weather:8100/mcp" || len(sc.Categories) != 1 || sc.Categories[0] != "weather" {
		t.Fatalf("weather = %+v (present %v)", sc, ok)
	}
	if _, ok := mustToolConfig(t, config.Config{}).Servers[mcp.WeatherServer]; ok {
		t.Fatal("weather registered without its URL")
	}
	// compose sets the URL on every install: without a token it stays off.
	if _, ok := mustToolConfig(t, config.Config{WeatherURL: "http://weather:8100"}).Servers[mcp.WeatherServer]; ok {
		t.Fatal("weather registered without its token")
	}
	// An mcp.json override replaces the endpoint, not when it is offered.
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {"weather": {"type": "http", "url": "https://other.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tc, err = toolConfigForConfig(config.Config{WeatherURL: "http://weather:8100", WeatherToken: strings.Repeat("t", 32), MCPServersFile: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc := tc.FileServers.Servers[mcp.WeatherServer]; sc.URL != "https://other.example/mcp" || len(sc.Categories) != 1 {
		t.Fatalf("override = %+v, want its URL and the built-in's categories", sc)
	}
}

// An upgraded install that still sets the Tavily key, but no sidecar, is told
// where web search went.
func TestWarnOrphanedSearchKey(t *testing.T) {
	key := func(string) string { return "set" }
	none := func(string) string { return "" }
	if !warnOrphanedSearchKey(config.Config{}, key) {
		t.Error("orphaned key not reported")
	}
	if warnOrphanedSearchKey(config.Config{SearchURL: "http://search:8030"}, key) || warnOrphanedSearchKey(config.Config{}, none) {
		t.Error("warned without cause")
	}
}

// The docgen sidecar is a best-effort built-in that may make artifacts.
func TestToolConfigForConfigAddsDocgenSidecar(t *testing.T) {
	tc, err := toolConfigForConfig(config.Config{DocgenURL: "http://docgen:8020/", DocgenToken: strings.Repeat("t", 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := tc.FileServers.Servers[mcp.DocgenServer]
	if !ok || !sc.BuiltIn || !sc.Artifacts || sc.URL != "http://docgen:8020/mcp" || sc.Headers["Authorization"] != "Bearer "+strings.Repeat("t", 32) {
		t.Fatalf("docgen = %+v (present %v)", sc, ok)
	}
	if _, required := tc.Required.Servers[mcp.DocgenServer]; required {
		t.Fatal("docgen sidecar must not be required")
	}
	if _, ok := mustToolConfig(t, config.Config{}).Servers[mcp.DocgenServer]; ok {
		t.Fatal("docgen registered without its URL")
	}
}

// An install run without compose that still names Gotenberg, but no
// sidecar, is told where the document tools went.
func TestWarnOrphanedGotenbergURL(t *testing.T) {
	url := func(string) string { return "http://gotenberg:3000" }
	none := func(string) string { return "" }
	if !warnOrphanedGotenbergURL(config.Config{}, url) {
		t.Error("orphaned URL not reported")
	}
	if warnOrphanedGotenbergURL(config.Config{DocgenURL: "http://docgen:8020"}, url) || warnOrphanedGotenbergURL(config.Config{}, none) {
		t.Error("warned without cause")
	}
}

func TestServerToolCount(t *testing.T) {
	tools := []llm.Tool{
		{Function: llm.ToolFunction{Name: "docgen__create_pdf_file"}},
		{Function: llm.ToolFunction{Name: "docgen__create_text_file"}},
		{Function: llm.ToolFunction{Name: "search__web"}},
		{Function: llm.ToolFunction{Name: "create_pdf_file"}},
	}
	if got := serverToolCount(tools, mcp.DocgenServer); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
}

// The image sidecar is a best-effort built-in that may make artifacts.
func TestToolConfigForConfigAddsImagegenSidecar(t *testing.T) {
	got := mustToolConfig(t, config.Config{ImageGenURL: "http://imagegen:8040", ImageGenToken: strings.Repeat("t", 32)})
	sc, ok := got.Servers[mcp.ImagegenServer]
	if !ok || !sc.Artifacts || !sc.BuiltIn || sc.URL != "http://imagegen:8040/mcp" || sc.Headers["Authorization"] != "Bearer "+strings.Repeat("t", 32) {
		t.Fatalf("imagegen = %+v (present %v)", sc, ok)
	}
	if _, ok := mustToolConfig(t, config.Config{}).Servers[mcp.ImagegenServer]; ok {
		t.Fatal("imagegen registered without its URL")
	}
}

// An upgraded install that still sets the fal key for loom is told where it went.
func TestWarnOrphanedImageKey(t *testing.T) {
	key := func(string) string { return "fal-key" }
	none := func(string) string { return "" }
	if !warnOrphanedImageKey(config.Config{}, key) {
		t.Error("orphaned key not reported")
	}
	if warnOrphanedImageKey(config.Config{ImageGenURL: "http://imagegen:8040"}, key) || warnOrphanedImageKey(config.Config{}, none) {
		t.Error("warned without cause")
	}
}

func TestToolConfigForConfigAddsFirstClassMCPTools(t *testing.T) {
	cfg := config.Config{
		ObscuraMCPURL:   "http://obscura:8090/mcp",
		ObscuraMCPToken: "obscura-token-0123456789abcdef0123",
	}

	got := mustToolConfig(t, cfg)
	fetch := got.Servers["fetch"]
	if fetch.Transport != mcp.TransportInProcess {
		t.Fatalf("fetch transport = %q, want in-process", fetch.Transport)
	}
	if fetch.URL != "" {
		t.Fatalf("fetch URL = %q, want empty (runs in-process)", fetch.URL)
	}
	if len(fetch.Tools) != 1 || fetch.Tools[0] != "fetch" {
		t.Fatalf("fetch tools = %#v, want [fetch]", fetch.Tools)
	}
	obscura := got.Servers["obscura"]
	if obscura.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("obscura transport = %q, want streamable-http", obscura.Transport)
	}
	if obscura.URL != "http://obscura:8090/mcp" {
		t.Fatalf("obscura URL = %q, want configured URL", obscura.URL)
	}
	wantObscuraTools := []string{"browser_navigate", "browser_snapshot"}
	if !reflect.DeepEqual(obscura.Tools, wantObscuraTools) {
		t.Fatalf("obscura tools = %#v, want %#v", obscura.Tools, wantObscuraTools)
	}
	if got := obscura.Headers["Authorization"]; got != "Bearer obscura-token-0123456789abcdef0123" {
		t.Fatalf("obscura Authorization = %q, want the configured token as bearer", got)
	}
}

func TestToolConfigForConfigTunewireIsBestEffortAndRestricted(t *testing.T) {
	const token = "tunewire-token-0123456789abcdef0123"
	got, err := toolConfigForConfig(config.Config{TunewireURL: "http://tunewire:8060/", TunewireToken: token, TunewireUsers: []string{"usr_1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{tunewireLibraryServer, tunewireCatalogServer} {
		if _, ok := got.Required.Servers[name]; ok {
			t.Errorf("%s is required: a dead sidecar would fail boot", name)
		}
		if got.FileServers.Servers[name].Headers["Authorization"] != "Bearer "+token {
			t.Errorf("%s auth = %q", name, got.FileServers.Servers[name].Headers["Authorization"])
		}
	}
	lib, cat := got.FileServers.Servers[tunewireLibraryServer], got.FileServers.Servers[tunewireCatalogServer]
	if lib.URL != "http://tunewire:8060/mcp/library" || !lib.RestrictUsers || !reflect.DeepEqual(lib.Users, []string{"usr_1"}) {
		t.Errorf("library = %+v", lib)
	}
	if cat.URL != "http://tunewire:8060/mcp/catalog" || cat.RestrictUsers {
		t.Errorf("catalog = %+v", cat)
	}
	// loom's own sidecar: labelled built-in, and its instructions (when to
	// search the catalog) reach the prompt.
	for _, sc := range []mcp.ServerConfig{lib, cat} {
		if !sc.BuiltIn || !sc.TrustInstructions {
			t.Errorf("%s: BuiltIn %v TrustInstructions %v", sc.URL, sc.BuiltIn, sc.TrustInstructions)
		}
	}
	// Library and catalog search only in music and recommendation turns.
	if !reflect.DeepEqual(lib.Categories, []string{"music", "entertainment_recs"}) || !reflect.DeepEqual(cat.Categories, []string{"music", "entertainment_recs"}) {
		t.Errorf("categories: library %v, catalog %v", lib.Categories, cat.Categories)
	}

	// An mcp.json override replaces the endpoint, but keeps the owner, even
	// when it names users of its own: the settings page owns the token.
	for _, entry := range []string{
		`{"type": "http", "url": "http://elsewhere:1/mcp"}`,
		`{"type": "http", "url": "http://elsewhere:1/mcp", "users": ["usr_carol"]}`,
	} {
		path := filepath.Join(t.TempDir(), "mcp.json")
		if err := os.WriteFile(path, []byte(`{"mcpServers": {"tunewire": `+entry+`}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		over, err := toolConfigForConfig(config.Config{TunewireURL: "http://tunewire:8060", TunewireToken: token, TunewireUsers: []string{"usr_1"}, MCPServersFile: path}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if o := over.FileServers.Servers[tunewireLibraryServer]; o.URL != "http://elsewhere:1/mcp" || !o.RestrictUsers || !o.PerUser || !reflect.DeepEqual(o.Users, []string{"usr_1"}) {
			t.Errorf("%s: override = %+v, want the file's URL, its users and per-user accounts", entry, o)
		}
	}

	off := mustToolConfig(t, config.Config{})
	if _, ok := off.Servers[tunewireLibraryServer]; ok {
		t.Error("tunewire registered without a URL")
	}
	if appleMusicService(config.Config{}) != nil {
		t.Error("settings service set without a URL")
	}
	if appleMusicService(config.Config{TunewireURL: "http://tunewire:8060", TunewireToken: token}) == nil {
		t.Error("settings service missing with a URL")
	}
}

func TestPushSubject(t *testing.T) {
	if got := pushSubject("https://loom.example.com"); got != "https://loom.example.com" {
		t.Errorf("https: %q", got)
	}
	if got := pushSubject("http://127.0.0.1:8080"); got != "mailto:loom@localhost" {
		t.Errorf("dev: %q", got)
	}
}

func TestToolConfigForConfigStravaIsBestEffortAndPerUser(t *testing.T) {
	const token = "strava-token-0123456789abcdef012345"
	got, err := toolConfigForConfig(config.Config{StravaURL: "http://strava:8050", StravaToken: token, StravaUsers: []string{"usr_1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, required := got.Required.Servers[stravaServer]; required {
		t.Error("strava is required: a dead sidecar would fail boot")
	}
	sc := got.FileServers.Servers[stravaServer]
	if sc.URL != "http://strava:8050/mcp" || !sc.PerUser || !sc.RestrictUsers || !reflect.DeepEqual(sc.Users, []string{"usr_1"}) {
		t.Errorf("strava = %+v", sc)
	}
	if _, ok := mustToolConfig(t, config.Config{}).Servers[stravaServer]; ok {
		t.Error("strava registered without a URL")
	}
	if stravaService(config.Config{}) != nil || stravaService(config.Config{StravaURL: "http://strava:8050", StravaToken: token}) == nil {
		t.Error("settings service on/off")
	}
}

func TestToolConfigForConfigMergesFileServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
	  "mcpServers": { "ipverse": { "type": "http", "url": "https://gateway.ipverse.net/mcp",
	    "headers": { "Authorization": "Bearer ${IPVERSE_API_KEY}" } } }
	}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv("IPVERSE_API_KEY", "ipv_secret")

	got := mustToolConfig(t, config.Config{MCPServersFile: path})
	ipverse, ok := got.Servers["ipverse"]
	if !ok {
		t.Fatalf("ipverse server missing: %#v", got.Servers)
	}
	if ipverse.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("ipverse transport = %q", ipverse.Transport)
	}
	if ipverse.Headers["Authorization"] != "Bearer ipv_secret" {
		t.Fatalf("ipverse Authorization = %q, want interpolated secret", ipverse.Headers["Authorization"])
	}
}

func TestToolConfigForConfigFileOverridesBuiltIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
	  "mcpServers": { "fetch": { "type": "http", "url": "http://override:9000/mcp" } }
	}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := mustToolConfig(t, config.Config{MCPServersFile: path})
	if got.Servers["fetch"].URL != "http://override:9000/mcp" {
		t.Fatalf("fetch URL = %q, want file override", got.Servers["fetch"].URL)
	}
}

func TestToolConfigForConfigSplitsFileServersBestEffort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
	  "mcpServers": { "ipverse": { "type": "http", "url": "https://gateway.ipverse.net/mcp" },
	    "fetch": { "type": "http", "url": "http://override:9000/mcp" } }
	}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := toolConfigForConfig(config.Config{MCPServersFile: path}, nil)
	if err != nil {
		t.Fatalf("toolConfigForConfig() error = %v", err)
	}
	// ipverse and the fetch override are best-effort (file-defined)...
	if _, ok := got.FileServers.Servers["ipverse"]; !ok {
		t.Errorf("ipverse missing from FileServers: %#v", got.FileServers.Servers)
	}
	if got.FileServers.Servers["fetch"].URL != "http://override:9000/mcp" {
		t.Errorf("fetch override missing from FileServers: %#v", got.FileServers.Servers)
	}
	// ...and the overridden built-in fetch is dropped from the required set so a
	// flaky override can never fail boot.
	if _, ok := got.Required.Servers["fetch"]; ok {
		t.Errorf("overridden built-in fetch still in Required: %#v", got.Required.Servers)
	}
}

func TestToolConfigForConfigPropagatesFileError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{ "mcpServers": { "x": { "type": "sse", "url": "https://x" } } }`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := toolConfigForConfig(config.Config{MCPServersFile: path}, nil); err == nil {
		t.Fatal("toolConfigForConfig() error = nil, want propagated file error")
	}
}
