package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandler_userAndFiles(t *testing.T) {
	h := New(Server{Name: "t", Version: "v1", Tools: []Tool{{
		Name: "who",
		Run: func(ctx context.Context, _ json.RawMessage) Result {
			return Result{Text: "user=" + User(ctx), Files: []File{{Name: "a.csv", MimeType: "text/csv", Text: "x,y\n1,2\n"}}}
		},
	}}})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"who"}}`

	_, r, raw := call(t, h, body, map[string]string{UserHeader: "  u-42 "})
	content, _ := r.Result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("want text + resource, got %s", raw)
	}
	if text := content[0].(map[string]any)["text"]; text != "user=u-42" {
		t.Errorf("user from header: %v", text)
	}
	res := content[1].(map[string]any)
	inner, _ := res["resource"].(map[string]any)
	if res["type"] != "resource" || inner["uri"] != "file:///a.csv" || inner["mimeType"] != "text/csv" || inner["text"] != "x,y\n1,2\n" {
		t.Errorf("resource: %+v", res)
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
