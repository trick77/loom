package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeEnv builds a lookupEnv func backed by a static map.
func fakeEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func writeTempFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLoadServersFromFile_httpWithEnvInterpolation(t *testing.T) {
	path := writeTempFile(t, `{
	  "mcpServers": {
	    "ipverse": {
	      "type": "http",
	      "url": "https://gateway.ipverse.net/mcp",
	      "headers": { "Authorization": "Bearer ${IPVERSE_API_KEY}" }
	    }
	  }
	}`)

	servers, err := LoadServersFromFile(path, fakeEnv(map[string]string{"IPVERSE_API_KEY": "ipv_secret"}))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v", err)
	}
	got, ok := servers["ipverse"]
	if !ok {
		t.Fatalf("server %q missing from %v", "ipverse", servers)
	}
	if got.Transport != TransportStreamableHTTP {
		t.Errorf("Transport = %q, want %q", got.Transport, TransportStreamableHTTP)
	}
	if got.URL != "https://gateway.ipverse.net/mcp" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Headers["Authorization"] != "Bearer ipv_secret" {
		t.Errorf("Authorization = %q, want interpolated secret", got.Headers["Authorization"])
	}
}

func TestLoadServersFromFile_stdioTransport(t *testing.T) {
	path := writeTempFile(t, `{
	  "mcpServers": {
	    "local": { "type": "stdio", "command": "my-server", "args": ["--flag"] }
	  }
	}`)

	servers, err := LoadServersFromFile(path, fakeEnv(nil))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v", err)
	}
	got := servers["local"]
	if got.Transport != TransportStdio {
		t.Errorf("Transport = %q, want %q", got.Transport, TransportStdio)
	}
	if got.Command != "my-server" || len(got.Args) != 1 || got.Args[0] != "--flag" {
		t.Errorf("command/args = %q %v", got.Command, got.Args)
	}
}

func TestLoadServersFromFile_missingEnvVarIsError(t *testing.T) {
	path := writeTempFile(t, `{
	  "mcpServers": { "ipverse": { "type": "http", "url": "https://x/mcp",
	    "headers": { "Authorization": "Bearer ${IPVERSE_API_KEY}" } } }
	}`)

	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want error naming the unset variable")
	}
}

func TestLoadServersFromFile_unsupportedType(t *testing.T) {
	path := writeTempFile(t, `{ "mcpServers": { "x": { "type": "sse", "url": "https://x" } } }`)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want error for unsupported type")
	}
}

func TestLoadServersFromFile_httpRequiresURL(t *testing.T) {
	path := writeTempFile(t, `{ "mcpServers": { "x": { "type": "http" } } }`)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want error for missing url")
	}
}

func TestLoadServersFromFile_stdioRequiresCommand(t *testing.T) {
	path := writeTempFile(t, `{ "mcpServers": { "x": { "type": "stdio" } } }`)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want error for missing command")
	}
}

func TestLoadServersFromFile_absentPathIsNoOp(t *testing.T) {
	servers, err := LoadServersFromFile(filepath.Join(t.TempDir(), "does-not-exist.json"), fakeEnv(nil))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v, want nil for absent file", err)
	}
	if servers != nil {
		t.Errorf("servers = %v, want nil", servers)
	}
}

func TestLoadServersFromFile_emptyPathIsNoOp(t *testing.T) {
	servers, err := LoadServersFromFile("", fakeEnv(nil))
	if err != nil || servers != nil {
		t.Fatalf("LoadServersFromFile(\"\") = (%v, %v), want (nil, nil)", servers, err)
	}
}

func TestLoadServersFromFile_malformedJSONIsError(t *testing.T) {
	path := writeTempFile(t, `{ not json `)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want error for malformed JSON")
	}
}

func TestLoadServersFromFile_defaultTypeIsStreamableHTTP(t *testing.T) {
	path := writeTempFile(t, `{ "mcpServers": { "x": { "url": "https://x/mcp" } } }`)
	servers, err := LoadServersFromFile(path, fakeEnv(nil))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v", err)
	}
	if servers["x"].Transport != TransportStreamableHTTP {
		t.Errorf("Transport = %q, want default %q", servers["x"].Transport, TransportStreamableHTTP)
	}
}
