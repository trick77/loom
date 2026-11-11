package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// A tool only reads when its server is configured read-only, or it says
// readOnlyHint and its server is trusted; any other, an unknown name
// included, may act.
func TestServiceReadsOnly(t *testing.T) {
	service, err := NewRequiredServiceFromClients(context.Background(), map[string]Client{
		"lib": &fakeClient{tools: []Tool{{Name: "lib__find", OriginalName: "find"}}},
		"acme": &fakeClient{tools: []Tool{
			{Name: "acme__list", OriginalName: "list", ReadOnly: true},
			{Name: "acme__send", OriginalName: "send"},
		}},
		"shady": &fakeClient{tools: []Tool{{Name: "shady__send", OriginalName: "send", ReadOnly: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.cfg = Config{Servers: map[string]ServerConfig{"lib": {ReadOnly: true}, "acme": {TrustInstructions: true}, "shady": {}}}
	for name, want := range map[string]bool{
		"lib__find":   true,
		"acme__list":  true,
		"acme__send":  false,
		"shady__send": false,
		"lib__gone":   false,
		"nobody":      false,
	} {
		if got := service.ReadsOnly(name); got != want {
			t.Errorf("ReadsOnly(%s) = %v, want %v", name, got, want)
		}
	}
	if (*Service)(nil).ReadsOnly("lib__find") {
		t.Error("a nil service vouched for a tool")
	}
}

// The readOnlyHint annotation reaches the exposed tool.
func TestExposeToolsCarriesReadOnlyHint(t *testing.T) {
	var tools []toolResult
	if err := json.Unmarshal([]byte(`[{"name":"list","annotations":{"readOnlyHint":true}},{"name":"send"}]`), &tools); err != nil {
		t.Fatal(err)
	}
	got := exposeTools("acme", ServerConfig{}, tools)
	if len(got) != 2 || !got[0].ReadOnly || got[1].ReadOnly {
		t.Fatalf("exposed = %+v", got)
	}
}

// mcp.json marks a read-only server with "readOnly": true; without it a
// server is not.
func TestLoadServersFromFileReadOnly(t *testing.T) {
	path := writeTempFile(t, `{"mcpServers": {
	  "peeq": {"type": "http", "url": "https://peeq.example/mcp", "readOnly": true},
	  "acme": {"type": "http", "url": "https://acme.example/mcp"}
	}}`)
	servers, err := LoadServersFromFile(path, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !servers["peeq"].ReadOnly || servers["acme"].ReadOnly {
		t.Fatalf("peeq %v, acme %v", servers["peeq"].ReadOnly, servers["acme"].ReadOnly)
	}
}

// loom's own reading servers are read-only; the ones that make files and
// images are not.
func TestBuiltInServersReadOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  ServerConfig
		want bool
	}{
		"fetch":      {FetchServerConfig(nil), true},
		"obscura":    {ObscuraServerConfig("http://o", "t"), true},
		"search":     {SearchServerConfig("http://s", "t"), true},
		"weather":    {WeatherServerConfig("http://w", "t"), true},
		"strava":     {StravaServerConfig("http://s", "t", nil), true},
		"tunewire":   {TunewireLibraryServerConfig("http://t", "t", nil), true},
		"applemusic": {TunewireCatalogServerConfig("http://t", "t"), true},
		"imagegen":   {ImagegenServerConfig("http://i", "t"), false},
		"docgen":     {DocgenServerConfig("http://d", "t"), false},
	} {
		if tc.cfg.ReadOnly != tc.want {
			t.Errorf("%s ReadOnly = %v, want %v", name, tc.cfg.ReadOnly, tc.want)
		}
	}
}
