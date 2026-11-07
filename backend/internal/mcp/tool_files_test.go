package mcp

import (
	"encoding/json"
	"testing"
)

func TestToolFilesKeepOnlyPlainCSVAndJSON(t *testing.T) {
	raw := `{"content":[
		{"type":"text","text":"analysis"},
		{"type":"resource","resource":{"uri":"file:///strava_1_streams.csv","mimeType":"text/csv","text":"a,b\n1,2\n"}},
		{"type":"resource","resource":{"uri":"file:///weeks.json","mimeType":"application/json","text":"[]"}},
		{"type":"resource","resource":{"uri":"file:///../etc/passwd.csv","mimeType":"text/csv","text":"x"}},
		{"type":"resource","resource":{"uri":"file:///a..b.csv","mimeType":"text/csv","text":"x"}},
		{"type":"resource","resource":{"uri":"file:///.hidden.csv","mimeType":"text/csv","text":"x"}},
		{"type":"resource","resource":{"uri":"file:///script.py","mimeType":"text/x-python","text":"x"}},
		{"type":"resource","resource":{"uri":"file:///wrong.json","mimeType":"text/csv","text":"x"}},
		{"type":"resource","resource":{"uri":"https://example.com/x.csv","mimeType":"text/csv","text":"x"}},
		{"type":"resource","resource":{"uri":"file:///empty.csv","mimeType":"text/csv","text":""}},
		{"type":"resource"}
	]}`
	var r callToolResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	got := r.toCallResult(false)
	if got.Text != "analysis" {
		t.Errorf("text = %q: a resource must not join the text", got.Text)
	}
	if len(got.Files) != 2 || got.Files[0].Name != "strava_1_streams.csv" || string(got.Files[0].Data) != "a,b\n1,2\n" ||
		got.Files[1].Name != "weeks.json" || got.Files[1].MimeType != "application/json" {
		t.Fatalf("files = %+v", got.Files)
	}
}

func TestToolFilesCapped(t *testing.T) {
	var content []toolContent
	for range maxToolFiles + 3 {
		content = append(content, toolContent{Type: "resource", Resource: &toolResource{URI: "file:///a.csv", MimeType: "text/csv", Text: "x"}})
	}
	if n := len(toolFiles(content)); n != maxToolFiles {
		t.Errorf("%d files, want %d", n, maxToolFiles)
	}
}
