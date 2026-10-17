package mcp

import "testing"

func TestLoadServersFromFile_usersAllowlist(t *testing.T) {
	path := writeTempFile(t, `{
	  "mcpServers": {
	    "peeq": { "url": "https://peeq.example/mcp", "users": "${PEEQ_USERS}", "trustInstructions": true },
	    "listed": { "url": "https://a.example/mcp", "users": ["usr_1", " sub-2 "] },
	    "blank": { "url": "https://b.example/mcp", "users": "${EMPTY}" },
	    "null": { "url": "https://n.example/mcp", "users": null },
	    "open": { "url": "https://c.example/mcp" }
	  }
	}`)
	servers, err := LoadServersFromFile(path, fakeEnv(map[string]string{"PEEQ_USERS": "usr_1, sub-9 ,", "EMPTY": ""}))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v", err)
	}
	if got := servers["peeq"]; !got.RestrictUsers || !got.TrustInstructions || len(got.Users) != 2 || got.Users[0] != "usr_1" || got.Users[1] != "sub-9" {
		t.Fatalf("peeq = %#v", got)
	}
	if got := servers["listed"]; !got.RestrictUsers || len(got.Users) != 2 || got.Users[1] != "sub-2" {
		t.Fatalf("listed users = %#v", got)
	}
	for _, name := range []string{"blank", "null"} {
		if got := servers[name]; !got.RestrictUsers || len(got.Users) != 0 {
			t.Fatalf("%s users must fail closed, got %#v", name, got)
		}
	}
	if got := servers["open"]; got.RestrictUsers || got.TrustInstructions {
		t.Fatalf("open server restricted or trusted: %#v", got)
	}
}

func TestLoadServersFromFile_usersRejectsWrongType(t *testing.T) {
	path := writeTempFile(t, `{"mcpServers": {"x": {"url": "https://x.example/mcp", "users": 3}}}`)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want a users type error")
	}
}
