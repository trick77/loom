package mcp

import (
	"net/url"
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

func TestTavilyServerConfigEmbedsKeyAndAllowlist(t *testing.T) {
	cfg := TavilyServerConfig("https://mcp.tavily.com/mcp/", "tvly-secret")
	if cfg.Transport != TransportStreamableHTTP {
		t.Fatalf("Transport = %q, want streamable-http", cfg.Transport)
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if got := u.Query().Get("tavilyApiKey"); got != "tvly-secret" {
		t.Fatalf("tavilyApiKey = %q, want tvly-secret", got)
	}
	if !reflect.DeepEqual(cfg.Tools, []string{"tavily_search"}) {
		t.Fatalf("Tools = %#v, want [tavily_search]", cfg.Tools)
	}
}

func TestTavilyServerConfigDefaultsURLWhenEmpty(t *testing.T) {
	cfg := TavilyServerConfig("", "k")
	u, err := url.Parse(cfg.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if u.Host != "mcp.tavily.com" {
		t.Fatalf("host = %q, want mcp.tavily.com", u.Host)
	}
}

func TestFetchServerConfigAllowlistsFetchTool(t *testing.T) {
	cfg := FetchServerConfig("http://fetch:8090/mcp")
	if cfg.Transport != TransportStreamableHTTP {
		t.Fatalf("Transport = %q, want streamable-http", cfg.Transport)
	}
	if cfg.URL != "http://fetch:8090/mcp" {
		t.Fatalf("URL = %q, want configured URL", cfg.URL)
	}
	if !reflect.DeepEqual(cfg.Tools, []string{"fetch"}) {
		t.Fatalf("Tools = %#v, want [fetch]", cfg.Tools)
	}
}

func TestObscuraServerConfigExposesAdvertisedTools(t *testing.T) {
	cfg := ObscuraServerConfig("http://obscura:8090/mcp")
	if cfg.Transport != TransportStreamableHTTP {
		t.Fatalf("Transport = %q, want streamable-http", cfg.Transport)
	}
	if cfg.URL != "http://obscura:8090/mcp" {
		t.Fatalf("URL = %q, want configured URL", cfg.URL)
	}
	if len(cfg.Tools) != 0 {
		t.Fatalf("Tools = %#v, want no allowlist", cfg.Tools)
	}
}
