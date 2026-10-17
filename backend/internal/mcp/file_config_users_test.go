package mcp

import "testing"

func TestLoadServersFromFile_usersAllowlist(t *testing.T) {
	path := writeTempFile(t, `{
	  "mcpServers": {
	    "peeq": { "url": "https://peeq.example/mcp", "users": "${PEEQ_USERS}" },
	    "listed": { "url": "https://a.example/mcp", "users": ["jan", " eve "] },
	    "blank": { "url": "https://b.example/mcp", "users": "${EMPTY}" },
	    "open": { "url": "https://c.example/mcp" }
	  }
	}`)
	servers, err := LoadServersFromFile(path, fakeEnv(map[string]string{"PEEQ_USERS": "jan, anna ,", "EMPTY": ""}))
	if err != nil {
		t.Fatalf("LoadServersFromFile() error = %v", err)
	}
	if got := servers["peeq"]; !got.RestrictUsers || len(got.Users) != 2 || got.Users[0] != "jan" || got.Users[1] != "anna" {
		t.Fatalf("peeq users = %#v", got)
	}
	if got := servers["listed"]; !got.RestrictUsers || len(got.Users) != 2 || got.Users[1] != "eve" {
		t.Fatalf("listed users = %#v", got)
	}
	if got := servers["blank"]; !got.RestrictUsers || len(got.Users) != 0 {
		t.Fatalf("blank users must fail closed, got %#v", got)
	}
	if got := servers["open"]; got.RestrictUsers {
		t.Fatalf("open server restricted: %#v", got)
	}
}

func TestLoadServersFromFile_usersRejectsWrongType(t *testing.T) {
	path := writeTempFile(t, `{"mcpServers": {"x": {"url": "https://x.example/mcp", "users": 3}}}`)
	if _, err := LoadServersFromFile(path, fakeEnv(nil)); err == nil {
		t.Fatal("LoadServersFromFile() error = nil, want a users type error")
	}
}
