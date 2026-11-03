package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandler_userFromHeader(t *testing.T) {
	h := New(Server{Name: "t", Version: "v1", Tools: []Tool{{
		Name: "who",
		Run: func(ctx context.Context, _ json.RawMessage) Result {
			return Result{Text: "user=" + User(ctx)}
		},
	}}})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"who"}}`

	_, r, raw := call(t, h, body, map[string]string{UserHeader: "  u-42 "})
	content, _ := r.Result["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "user=u-42" {
		t.Fatalf("user from header: %s", raw)
	}
	_, _, raw = call(t, h, body, nil)
	if !strings.Contains(raw, `"user="`) {
		t.Errorf("no header must mean no user: %s", raw)
	}
}

func TestWithUser(t *testing.T) {
	if User(context.Background()) != "" {
		t.Error("empty context has a user")
	}
	if got := User(WithUser(context.Background(), "abc")); got != "abc" {
		t.Errorf("got %q", got)
	}
}
