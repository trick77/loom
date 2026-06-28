package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// TestBuildShareSnapshot_sanitizes is the security contract for sharing: a message
// carrying reasoning, a tool/activity trace, RAG citations naming a document, an
// uploaded attachment, token metrics, and a generated artifact must produce a blob
// that contains ONLY the text + the generated artifact (URL rewritten), and NONE of
// the private fields. If this test ever fails, sharing leaks private data.
func TestBuildShareSnapshot_sanitizes(t *testing.T) {
	promptTokens := 1234
	model := "secret-model-name"
	msgs := []chat.Message{
		{
			ID:          "m1",
			Role:        chat.RoleUser,
			Content:     "look at this file",
			Attachments: json.RawMessage(`[{"kind":"document","filename":"private-upload.pdf"}]`),
			CreatedAt:   time.Unix(1700000000, 0),
		},
		{
			ID:               "m2",
			Role:             chat.RoleAssistant,
			Content:          "here is the answer",
			ReasoningContent: "SECRET_CHAIN_OF_THOUGHT",
			Citations:        json.RawMessage(`[{"title":"CONFIDENTIAL_SOURCE_DOC","url":"doc://x"}]`),
			Artifacts:        json.RawMessage(`[{"id":"art1","displayFilename":"diagram.svg","downloadUrl":"/api/artifacts/art1/download"}]`),
			ContentBlocks: json.RawMessage(`[
				{"type":"trace","content":"SECRET_TOOL_TRACE"},
				{"type":"text","content":"here is the answer"},
				{"type":"artifact","artifact":{"id":"art2","displayFilename":"chart.png","downloadUrl":"/api/artifacts/art2/download","thumbnailUrl":"/api/artifacts/art2/thumbnail"}}
			]`),
			PromptTokens: &promptTokens,
			Model:        &model,
			CreatedAt:    time.Unix(1700000001, 0),
		},
	}

	snap, artifactIDs, err := buildShareSnapshot("SH4RE", "My Thread", "Jan", msgs)
	if err != nil {
		t.Fatalf("buildShareSnapshot: %v", err)
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(blob)

	mustContain := []string{
		"look at this file",
		"here is the answer",
		"art1", "art2", // generated artifacts kept
		"/api/shares/SH4RE/artifacts/art1/download",
		"/api/shares/SH4RE/artifacts/art2/download",
		"/api/shares/SH4RE/artifacts/art2/thumbnail",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot missing %q\nblob: %s", want, got)
		}
	}

	mustNotContain := []string{
		"SECRET_CHAIN_OF_THOUGHT",      // reasoning
		"SECRET_TOOL_TRACE",            // activity trace block
		"CONFIDENTIAL_SOURCE_DOC",      // RAG citation / document leak
		"private-upload.pdf",           // uploaded attachment file
		"secret-model-name",            // model metric
		"1234",                         // token metric
		"/api/artifacts/art1/download", // un-rewritten (authed) artifact URL
		`"trace"`,                      // no trace blocks survive
	}
	for _, bad := range mustNotContain {
		if strings.Contains(got, bad) {
			t.Errorf("snapshot LEAKED %q\nblob: %s", bad, got)
		}
	}

	// The user message that only carried an attachment + caption is kept with the
	// caption and an attachment marker; the assistant message is kept. Allowlist
	// must contain exactly the two generated artifacts.
	if len(snap.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(snap.Messages))
	}
	if !snap.Messages[0].HadAttachment {
		t.Errorf("user message should be flagged HadAttachment")
	}
	if len(artifactIDs) != 2 {
		t.Errorf("want 2 allowlisted artifact ids, got %v", artifactIDs)
	}
}

// TestBuildShareSnapshot_skipsNonUserAssistant ensures only user/assistant roles
// are serialized and image-only (empty) messages are dropped.
func TestBuildShareSnapshot_skipsNonUserAssistant(t *testing.T) {
	msgs := []chat.Message{
		{ID: "t", Role: chat.RoleTool, Content: "tool output", CreatedAt: time.Unix(1, 0)},
		{ID: "empty", Role: chat.RoleAssistant, Content: "", CreatedAt: time.Unix(2, 0)},
		{ID: "ok", Role: chat.RoleUser, Content: "hello", CreatedAt: time.Unix(3, 0)},
	}
	snap, _, err := buildShareSnapshot("S", "T", "A", msgs)
	if err != nil {
		t.Fatalf("buildShareSnapshot: %v", err)
	}
	if len(snap.Messages) != 1 || snap.Messages[0].ID != "ok" {
		t.Fatalf("want only the user message, got %+v", snap.Messages)
	}
}
