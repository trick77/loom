package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandler_filesAsResourcesWithinTheCap(t *testing.T) {
	big := strings.Repeat("x", maxFilesBytes)
	h := New(Server{Name: "t", Version: "v1", Tools: []Tool{{
		Name: "files",
		Run: func(context.Context, json.RawMessage) Result {
			return Result{Text: "here", Files: []File{
				{Name: "a.csv", MimeType: "text/csv", Text: "x,y\n1,2\n"},
				{Name: "big.csv", MimeType: "text/csv", Text: big},
				{Name: "b.json", MimeType: "application/json", Text: "{}"},
			}}
		},
	}}})
	_, r, raw := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files"}}`, nil)
	content, _ := r.Result["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("want text + 2 resources (big.csv over the cap), got %d", len(content))
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "not attached, too large: big.csv") {
		t.Errorf("text does not name the dropped file: %q", text)
	}
	res := content[1].(map[string]any)
	inner, _ := res["resource"].(map[string]any)
	if res["type"] != "resource" || inner["uri"] != "file:///a.csv" || inner["mimeType"] != "text/csv" || inner["text"] != "x,y\n1,2\n" {
		t.Errorf("resource: %+v", res)
	}
	if !strings.Contains(raw, `"uri":"file:///b.json"`) {
		t.Errorf("a small file after the dropped one is missing: %.200s", raw)
	}
	if len(raw) > 4<<20 {
		t.Errorf("response %d bytes, over loom's cap", len(raw))
	}
}

// A text at the cap still ends with the note on dropped files.
func TestHandler_droppedNoteSurvivesALongText(t *testing.T) {
	h := New(Server{Name: "t", Version: "v1", Tools: []Tool{{
		Name: "long",
		Run: func(context.Context, json.RawMessage) Result {
			return Result{Text: strings.Repeat("a line of analysis\n", 3000), Files: []File{{Name: "big.csv", MimeType: "text/csv", Text: strings.Repeat("x", maxFilesBytes+1)}}}
		},
	}}})
	_, r, _ := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"long"}}`, nil)
	text := r.Result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.HasSuffix(text, "(not attached, too large: big.csv)") || len(text) > maxToolTextBytes {
		t.Errorf("len %d, tail %q", len(text), text[len(text)-80:])
	}
}
