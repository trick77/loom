package mcp

import (
	"reflect"
	"testing"
)

func TestExposedToolNamePrefixesServerName(t *testing.T) {
	name := ExposedToolName("search", "web-search")
	if name != "search__web-search" {
		t.Fatalf("ExposedToolName() = %q, want search__web-search", name)
	}

	server, tool, ok := SplitExposedToolName(name)
	if !ok || server != "search" || tool != "web-search" {
		t.Fatalf("SplitExposedToolName() = %q %q %v", server, tool, ok)
	}
}

// The search sidecar is loom's own: bearer token, best-effort, and like any
// server its instructions stay out of the prompt.
func TestSearchServerConfig(t *testing.T) {
	cfg := SearchServerConfig("http://search:8030/", "tok")
	if cfg.Transport != TransportStreamableHTTP || cfg.URL != "http://search:8030/mcp" ||
		cfg.Headers["Authorization"] != "Bearer tok" || !cfg.BuiltIn || cfg.TrustInstructions || cfg.Artifacts {
		t.Fatalf("SearchServerConfig = %+v", cfg)
	}
}

func TestFetchServerConfigAllowlistsFetchTool(t *testing.T) {
	cfg := FetchServerConfig(nil)
	if cfg.Transport != TransportInProcess {
		t.Fatalf("Transport = %q, want in-process", cfg.Transport)
	}
	if cfg.URL != "" {
		t.Fatalf("URL = %q, want empty (runs in-process)", cfg.URL)
	}
	if !reflect.DeepEqual(cfg.Tools, []string{"fetch"}) {
		t.Fatalf("Tools = %#v, want [fetch]", cfg.Tools)
	}
}

func TestObscuraServerConfigAllowlistsNavigateAndSnapshot(t *testing.T) {
	cfg := ObscuraServerConfig("http://obscura:8090/mcp", "")
	if cfg.Transport != TransportStreamableHTTP {
		t.Fatalf("Transport = %q, want streamable-http", cfg.Transport)
	}
	if cfg.URL != "http://obscura:8090/mcp" {
		t.Fatalf("URL = %q, want configured URL", cfg.URL)
	}
	// The allowlist keeps the obscura surface to the two tools the fetch->obscura
	// fallback drives, instead of injecting the full ~20-tool browser surface into
	// every prompt.
	want := []string{"browser_navigate", "browser_snapshot"}
	if !reflect.DeepEqual(cfg.Tools, want) {
		t.Fatalf("Tools = %#v, want %#v", cfg.Tools, want)
	}
}

func TestObscuraServerConfigSendsTokenAsBearer(t *testing.T) {
	cfg := ObscuraServerConfig("http://obscura:8090/mcp", "tok-0123456789abcdef0123456789abcdef")
	want := map[string]string{"Authorization": "Bearer tok-0123456789abcdef0123456789abcdef"}
	if !reflect.DeepEqual(cfg.Headers, want) {
		t.Fatalf("Headers = %#v, want %#v", cfg.Headers, want)
	}
}
