package imagegen

import (
	"encoding/json"
	"testing"
)

// The image tool's schema rides on every chat turn where images can be
// stored, though image turns go through their own loop with their own
// shape guidance: its descriptions stay short.
func TestToolSchemaStaysSmall(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"description": toolDescription, "properties": toolProperties()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("image tool description+properties: %d bytes", len(raw))
	if len(raw) > 1500 {
		t.Errorf("image tool description+properties = %d bytes, want at most 1500", len(raw))
	}
}
