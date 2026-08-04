package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// The citations of a shared answer are the one place where private data is let out
// deliberately rather than dropped, so the rule is asserted directly: a public http(s)
// page may go, anything else may not, and only the whitelisted fields travel.
func TestProjectCitationsForShare_keepsOnlyPublicWebSources(t *testing.T) {
	raw := json.RawMessage(`[
		{"documentId":"d1","filename":"private-upload.pdf","snippet":"SECRET_FILE_CONTENTS","score":0.9,"index":1},
		{"documentId":"","filename":"Modal","snippet":"Modal runs Python.","score":0.8,"url":"https://modal.com/docs","index":2,"title":"Modal docs","favicon":"https://modal.com/f.ico"},
		{"documentId":"d2","filename":"INTERNAL_LOCATOR.docx","snippet":"SECRET_DOC","score":0.7,"url":"doc://internal/42","index":3}
	]`)

	projected, kept := projectCitationsForShare(raw)
	got := string(projected)

	for _, leak := range []string{
		"private-upload.pdf", "SECRET_FILE_CONTENTS",
		"INTERNAL_LOCATOR.docx", "SECRET_DOC", "doc://internal/42",
		"documentId", "score", "favicon",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("projection LEAKED %q\nblob: %s", leak, got)
		}
	}
	for _, want := range []string{"https://modal.com/docs", "Modal docs", "Modal runs Python."} {
		if !strings.Contains(got, want) {
			t.Errorf("projection missing %q\nblob: %s", want, got)
		}
	}

	// Only the web source's marker survives, so only [2] still resolves in the prose.
	if len(kept) != 1 || !kept[2] {
		t.Errorf("kept = %v, want only index 2", kept)
	}
}

// A url is not enough on its own: the UI treats "has a url" as "is a web source" for
// display, which is too loose for a boundary a stranger reads across.
func TestProjectCitationsForShare_rejectsNonWebSchemes(t *testing.T) {
	for _, url := range []string{"doc://x", "file:///etc/passwd", "", "not a url", "ftp://host/f"} {
		raw := json.RawMessage(`[{"filename":"SECRET","url":` + mustJSON(t, url) + `,"index":1}]`)
		projected, kept := projectCitationsForShare(raw)
		if projected != nil {
			t.Errorf("url %q was let through: %s", url, projected)
		}
		if len(kept) != 0 {
			t.Errorf("url %q kept markers: %v", url, kept)
		}
	}
}

func TestProjectCitationsForShare_emptyAndMalformed(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`[]`), json.RawMessage(`{"not":"an array"}`)} {
		projected, kept := projectCitationsForShare(raw)
		if projected != nil || len(kept) != 0 {
			t.Errorf("raw %s: got %s / %v, want nil / empty", raw, projected, kept)
		}
	}
}

func TestStripDroppedMarkers(t *testing.T) {
	cited := map[int]bool{1: true, 2: true, 3: true}
	kept := map[int]bool{2: true}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "drops a marker and the space in front of it",
			content: "The upload says so [1]. The page agrees [2].",
			want:    "The upload says so. The page agrees [2].",
		},
		{
			name:    "thins a run down to its surviving marker",
			content: "Both back this [1][2].",
			want:    "Both back this [2].",
		},
		{
			name:    "removes a run that loses every marker",
			content: "Only uploads back this [1][3].",
			want:    "Only uploads back this.",
		},
		{
			name:    "leaves brackets that were never citations",
			content: "See note [9] and arr[0] in prose [1].",
			want:    "See note [9] and arr[0] in prose.",
		},
		{
			name:    "leaves an index expression inside a fence",
			content: "Text [1].\n\n```go\nfmt.Println(arr[1])\n```\n",
			want:    "Text.\n\n```go\nfmt.Println(arr[1])\n```\n",
		},
		{
			name:    "leaves an inline code span alone",
			content: "Use `arr[1]` here [1].",
			want:    "Use `arr[1]` here.",
		},
		{
			name:    "leaves an indented code block alone",
			content: "Text [1].\n\n    value := arr[1]\n",
			want:    "Text.\n\n    value := arr[1]\n",
		},
		{
			name:    "leaves an unterminated fence alone",
			content: "Text [1].\n\n```\narr[1]\n",
			want:    "Text.\n\n```\narr[1]\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := stripDroppedMarkers(test.content, cited, kept); got != test.want {
				t.Errorf("got  %q\nwant %q", got, test.want)
			}
		})
	}
}

// Nothing dropped means nothing rewritten: an answer citing only web sources must
// come through byte for byte, brackets and code included.
func TestStripDroppedMarkers_untouchedWhenNothingDropped(t *testing.T) {
	content := "Both agree [1][2]. See `arr[1]` and note [7]."
	cited := map[int]bool{1: true, 2: true}
	kept := map[int]bool{1: true, 2: true}

	if got := stripDroppedMarkers(content, cited, kept); got != content {
		t.Errorf("content was rewritten:\ngot  %q\nwant %q", got, content)
	}
}

// End to end through the snapshot builder: the document's marker must be gone from
// BOTH the message content and the text block, which carries its own copy.
func TestBuildShareSnapshot_citationsAndMarkers(t *testing.T) {
	content := "The upload says so [1]. The page agrees [2]."
	msgs := []chat.Message{{
		ID:      "m1",
		Role:    chat.RoleAssistant,
		Content: content,
		Citations: json.RawMessage(`[
			{"documentId":"d1","filename":"private-upload.pdf","snippet":"SECRET_FILE_CONTENTS","score":0.9,"index":1},
			{"documentId":"","filename":"Modal","snippet":"Modal runs Python.","score":0.8,"url":"https://modal.com/docs","index":2,"title":"Modal docs"}
		]`),
		ContentBlocks: json.RawMessage(`[{"type":"text","content":` + mustJSON(t, content) + `}]`),
		CreatedAt:     time.Unix(1700000000, 0),
	}}

	snap, _, err := buildShareSnapshot("SH4RE", "T", "Jan", msgs)
	if err != nil {
		t.Fatalf("buildShareSnapshot: %v", err)
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(blob)

	want := "The upload says so. The page agrees [2]."
	if snap.Messages[0].Content != want {
		t.Errorf("content = %q, want %q", snap.Messages[0].Content, want)
	}
	if !strings.Contains(got, mustJSON(t, want)) {
		t.Errorf("text block still carries the dropped marker\nblob: %s", got)
	}
	for _, leak := range []string{"private-upload.pdf", "SECRET_FILE_CONTENTS"} {
		if strings.Contains(got, leak) {
			t.Errorf("snapshot LEAKED %q\nblob: %s", leak, got)
		}
	}
	if !strings.Contains(got, "https://modal.com/docs") {
		t.Errorf("snapshot dropped the web source\nblob: %s", got)
	}
}

// An answer that cited only private documents carries no citations field at all, so
// the viewer cannot tell it apart from an older snapshot — and its markers are gone.
func TestBuildShareSnapshot_documentOnlyAnswerCarriesNoCitations(t *testing.T) {
	msgs := []chat.Message{{
		ID:            "m1",
		Role:          chat.RoleAssistant,
		Content:       "The upload says so [1].",
		Citations:     json.RawMessage(`[{"documentId":"d1","filename":"private.pdf","snippet":"x","score":0.9,"index":1}]`),
		ContentBlocks: json.RawMessage(`[]`),
		CreatedAt:     time.Unix(1700000000, 0),
	}}

	snap, _, err := buildShareSnapshot("SH4RE", "T", "Jan", msgs)
	if err != nil {
		t.Fatalf("buildShareSnapshot: %v", err)
	}
	if snap.Messages[0].Citations != nil {
		t.Errorf("citations = %s, want nil", snap.Messages[0].Citations)
	}
	if want := "The upload says so."; snap.Messages[0].Content != want {
		t.Errorf("content = %q, want %q", snap.Messages[0].Content, want)
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), `"citations"`) {
		t.Errorf("empty citations field was emitted\nblob: %s", blob)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %q: %v", value, err)
	}
	return string(encoded)
}
