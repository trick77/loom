package httpapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

func TestCollectArtifactIDsAcrossBothEmbeddingSites(t *testing.T) {
	messages := []chat.Message{
		{
			ID:        "m1",
			Artifacts: json.RawMessage(`[{"id":"art_1","displayFilename":"a.pdf"}]`),
			ContentBlocks: json.RawMessage(`[
				{"type":"text","content":"hi"},
				{"type":"artifact","artifact":{"id":"art_2","displayFilename":"b.pdf"}},
				{"type":"artifact","artifact":{"id":"art_1","displayFilename":"a.pdf"}}
			]`),
		},
		{ID: "m2", Artifacts: json.RawMessage(`[]`), ContentBlocks: json.RawMessage(`null`)},
	}

	ids, err := collectArtifactIDs(messages)
	if err != nil {
		t.Fatalf("collectArtifactIDs() error = %v", err)
	}
	sort.Strings(ids)
	if want := []string{"art_1", "art_2"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %#v, want %#v (deduped across artifacts array and content blocks)", ids, want)
	}
}

func TestOverlayMessageArtifactsRename(t *testing.T) {
	messages := []chat.Message{{
		ID:            "m1",
		Artifacts:     json.RawMessage(`[{"id":"art_1","displayFilename":"old.pdf","downloadUrl":"/api/artifacts/art_1/download"}]`),
		ContentBlocks: json.RawMessage(`[{"type":"artifact","artifact":{"id":"art_1","displayFilename":"old.pdf"}}]`),
	}}
	byID := map[string]artifact.Artifact{
		"art_1": {ID: "art_1", DisplayFilename: "new.pdf"},
	}

	if err := overlayMessageArtifacts(messages, byID); err != nil {
		t.Fatalf("overlayMessageArtifacts() error = %v", err)
	}

	// New name lands in both embedding sites; the downloadUrl snapshot is preserved.
	if got := firstArtifactField(t, messages[0].Artifacts, "displayFilename"); got != "new.pdf" {
		t.Fatalf("artifacts displayFilename = %q, want new.pdf", got)
	}
	if got := firstArtifactField(t, messages[0].Artifacts, "downloadUrl"); got != "/api/artifacts/art_1/download" {
		t.Fatalf("artifacts downloadUrl = %q, want preserved", got)
	}
	if got := firstArtifactField(t, messages[0].Artifacts, "deleted"); got != "false" {
		t.Fatalf("artifacts deleted = %q, want false", got)
	}
	if got := firstBlockArtifactField(t, messages[0].ContentBlocks, "displayFilename"); got != "new.pdf" {
		t.Fatalf("content block displayFilename = %q, want new.pdf", got)
	}
}

func TestOverlayMessageArtifactsDeleted(t *testing.T) {
	messages := []chat.Message{{
		ID:        "m1",
		Artifacts: json.RawMessage(`[{"id":"art_1","displayFilename":"old.pdf"}]`),
	}}
	byID := map[string]artifact.Artifact{
		"art_1": {ID: "art_1", DisplayFilename: "old.pdf", Deleted: true},
	}

	if err := overlayMessageArtifacts(messages, byID); err != nil {
		t.Fatalf("overlayMessageArtifacts() error = %v", err)
	}
	if got := firstArtifactField(t, messages[0].Artifacts, "deleted"); got != "true" {
		t.Fatalf("artifacts deleted = %q, want true", got)
	}
}

func TestOverlayMessageArtifactsUnknownIDTreatedAsDeleted(t *testing.T) {
	messages := []chat.Message{{
		ID:        "m1",
		Artifacts: json.RawMessage(`[{"id":"art_gone","displayFilename":"ghost.pdf"}]`),
	}}

	// Empty lookup: the artifact was purged and is no longer accessible.
	if err := overlayMessageArtifacts(messages, map[string]artifact.Artifact{}); err != nil {
		t.Fatalf("overlayMessageArtifacts() error = %v", err)
	}
	if got := firstArtifactField(t, messages[0].Artifacts, "deleted"); got != "true" {
		t.Fatalf("unknown-id deleted = %q, want true", got)
	}
	// The last-known snapshot name is preserved so the tombstone can label the file.
	if got := firstArtifactField(t, messages[0].Artifacts, "displayFilename"); got != "ghost.pdf" {
		t.Fatalf("unknown-id displayFilename = %q, want ghost.pdf preserved", got)
	}
}

func firstArtifactField(t *testing.T, raw json.RawMessage, field string) string {
	t.Helper()
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		t.Fatalf("unmarshal artifacts: %v", err)
	}
	if len(objs) == 0 {
		t.Fatalf("no artifact objects in %s", raw)
	}
	return rawFieldString(objs[0][field])
}

func firstBlockArtifactField(t *testing.T, raw json.RawMessage, field string) string {
	t.Helper()
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	for _, block := range blocks {
		if blockType(block) != "artifact" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(block["artifact"], &obj); err != nil {
			t.Fatalf("unmarshal block artifact: %v", err)
		}
		return rawFieldString(obj[field])
	}
	t.Fatalf("no artifact block in %s", raw)
	return ""
}

// rawFieldString returns a JSON string field's value, or the raw token for
// non-strings (e.g. "true"/"false"), so one helper serves both.
func rawFieldString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
