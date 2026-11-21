package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// A trusted server's explicit readOnlyHint: false beats its server-wide
// readOnly: an mcp.json written before the server grew an acting tool must
// not let that tool run as a read.
func TestReadsOnlyExplicitActingHintWins(t *testing.T) {
	service, err := NewRequiredServiceFromClients(context.Background(), map[string]Client{
		"peeq": &fakeClient{tools: []Tool{
			{Name: "peeq__search", OriginalName: "search", ReadOnly: true},
			{Name: "peeq__add", OriginalName: "add", Acting: true},
		}},
		"shady": &fakeClient{tools: []Tool{{Name: "shady__send", OriginalName: "send", Acting: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.cfg = Config{Servers: map[string]ServerConfig{
		"peeq":  {ReadOnly: true, TrustInstructions: true},
		"shady": {ReadOnly: true},
	}}
	for name, want := range map[string]bool{
		"peeq__search": true,
		"peeq__add":    false,
		// An untrusted server's hints count for nothing either way: the
		// operator's readOnly stands.
		"shady__send": true,
	} {
		if got := service.ReadsOnly(name); got != want {
			t.Errorf("ReadsOnly(%s) = %v, want %v", name, got, want)
		}
	}
}

// readOnlyHint: false is told apart from no hint at all.
func TestExposeToolsCarriesActingHint(t *testing.T) {
	var tools []toolResult
	if err := json.Unmarshal([]byte(`[{"name":"add","annotations":{"readOnlyHint":false}},{"name":"send"}]`), &tools); err != nil {
		t.Fatal(err)
	}
	got := exposeTools("peeq", ServerConfig{}, tools)
	if len(got) != 2 || !got[0].Acting || got[1].Acting {
		t.Fatalf("exposed = %+v", got)
	}
}

// Only an acting tool of a confirm server waits for the user's click.
func TestServiceConfirms(t *testing.T) {
	service, err := NewRequiredServiceFromClients(context.Background(), map[string]Client{
		"peeq": &fakeClient{tools: []Tool{
			{Name: "peeq__search", OriginalName: "search", ReadOnly: true},
			{Name: "peeq__add", OriginalName: "add", Acting: true},
		}},
		"acme": &fakeClient{tools: []Tool{{Name: "acme__send", OriginalName: "send"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.cfg = Config{Servers: map[string]ServerConfig{
		"peeq": {Confirm: true, TrustInstructions: true},
		"acme": {},
	}}
	for name, want := range map[string]bool{
		"peeq__search": false,
		"peeq__add":    true,
		"acme__send":   false,
		"nobody":       false,
	} {
		if got := service.Confirms(name); got != want {
			t.Errorf("Confirms(%s) = %v, want %v", name, got, want)
		}
	}
	if (*Service)(nil).Confirms("peeq__add") {
		t.Error("a nil service asked for a click")
	}
}

// mcp.json turns the click on with "confirm": true.
func TestLoadServersFromFileConfirm(t *testing.T) {
	path := writeTempFile(t, `{"mcpServers": {
	  "peeq": {"type": "http", "url": "https://peeq.example/mcp", "confirm": true},
	  "acme": {"type": "http", "url": "https://acme.example/mcp"}
	}}`)
	servers, err := LoadServersFromFile(path, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !servers["peeq"].Confirm || servers["acme"].Confirm {
		t.Fatalf("peeq %v, acme %v", servers["peeq"].Confirm, servers["acme"].Confirm)
	}
}

// structuredContent.items and .link are the confirmation card's result: a
// status per element of the call's array argument and one link to open. A
// status loom does not know, a link that is not http(s), anything past
// maxActionItems and text past its cap are dropped or cut.
func TestStructuredActionResult(t *testing.T) {
	long := make([]byte, maxActionMessageRunes+50)
	for i := range long {
		long[i] = 'x'
	}
	raw := json.RawMessage(`{"items":[
	  {"status":"done","message":"queued"},
	  {"status":"skipped","message":"already in library"},
	  {"status":"failed","message":"` + string(long) + `"},
	  {"status":"exploded","message":"?"}
	],"link":{"url":"https://peeq.example/activity","label":"peeq"}}`)
	items, link := structuredActionResult(raw)
	if len(items) != 4 || items[0] != (ActionItem{Status: "done", Message: "queued"}) || items[1].Status != "skipped" {
		t.Fatalf("items = %+v", items)
	}
	if items[3].Status != "" || len([]rune(items[2].Message)) != maxActionMessageRunes {
		t.Errorf("unknown status kept or long message not cut: %+v", items[2:])
	}
	if link == nil || link.URL != "https://peeq.example/activity" || link.Label != "peeq" {
		t.Errorf("link = %+v", link)
	}
	for _, bad := range []string{
		`{"link":{"url":"javascript:alert(1)","label":"x"}}`,
		`{"link":{"url":"","label":"x"}}`,
		`"just text"`,
		`{"items":"nope"}`,
	} {
		if items, link := structuredActionResult(json.RawMessage(bad)); len(items) != 0 || link != nil {
			t.Errorf("%s: items %+v, link %+v", bad, items, link)
		}
	}
}
