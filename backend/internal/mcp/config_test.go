package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigParsesRemoteAndStdioServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
		"servers": {
			"search": {
				"transport": "streamable-http",
				"url": "http://search-mcp:8080/mcp",
				"headers": {"Authorization": "Bearer token"}
			},
			"fetch": {
				"transport": "stdio",
				"command": "fetch-mcp",
				"args": ["--port", "0"],
				"env": {"FETCH_TIMEOUT": "5s"}
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if len(cfg.Servers) != 2 {
		t.Fatalf("len(Servers) = %d, want 2", len(cfg.Servers))
	}
	search := cfg.Servers["search"]
	if search.Transport != TransportStreamableHTTP {
		t.Fatalf("search transport = %q, want %q", search.Transport, TransportStreamableHTTP)
	}
	if search.URL != "http://search-mcp:8080/mcp" {
		t.Fatalf("search URL = %q", search.URL)
	}
	if search.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("search header = %#v", search.Headers)
	}
	fetch := cfg.Servers["fetch"]
	if fetch.Transport != TransportStdio {
		t.Fatalf("fetch transport = %q, want %q", fetch.Transport, TransportStdio)
	}
	if fetch.Command != "fetch-mcp" || len(fetch.Args) != 2 || fetch.Args[0] != "--port" {
		t.Fatalf("fetch command/args = %q %#v", fetch.Command, fetch.Args)
	}
	if fetch.Env["FETCH_TIMEOUT"] != "5s" {
		t.Fatalf("fetch env = %#v", fetch.Env)
	}
}

func TestLoadConfigAcceptsLegacyHTTPTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
		"servers": {
			"search": {"transport": "http", "url": "http://search-mcp:8080/mcp"}
		}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if cfg.Servers["search"].Transport != TransportStreamableHTTP {
		t.Fatalf("transport = %q, want normalized streamable-http", cfg.Servers["search"].Transport)
	}
}

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

func TestLoadConfigRejectsInvalidServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
		"servers": {
			"bad": {"transport": "streamable-http"}
		}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig() error = nil, want missing URL error")
	}
}
