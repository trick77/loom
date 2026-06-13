package docgen

import "testing"

// MiMo frequently serializes the blocks array as a JSON-encoded string rather than
// a real array; parseBlocks must decode that form instead of dropping the document.
func TestParseBlocks_AcceptsJSONEncodedString(t *testing.T) {
	payload := map[string]any{
		"blocks": `[{"type":"heading","level":1,"text":"Title"},{"type":"paragraph","text":"Body"}]`,
	}
	blocks := parseBlocks(payload)
	if len(blocks) != 2 {
		t.Fatalf("parseBlocks returned %d blocks, want 2", len(blocks))
	}
	if blocks[0].Type != "heading" || blocks[0].Level != 1 || blocks[0].Text != "Title" {
		t.Fatalf("block[0] = %+v, want heading/level1/Title", blocks[0])
	}
	if blocks[1].Type != "paragraph" || blocks[1].Text != "Body" {
		t.Fatalf("block[1] = %+v, want paragraph/Body", blocks[1])
	}
}

func TestParseBlocks_StillAcceptsRealArray(t *testing.T) {
	payload := map[string]any{
		"blocks": []any{
			map[string]any{"type": "paragraph", "text": "Hello"},
		},
	}
	if blocks := parseBlocks(payload); len(blocks) != 1 || blocks[0].Text != "Hello" {
		t.Fatalf("parseBlocks(real array) = %+v, want one paragraph 'Hello'", blocks)
	}
}

func TestParseBlocks_GarbageStringReturnsNil(t *testing.T) {
	if blocks := parseBlocks(map[string]any{"blocks": "not json"}); blocks != nil {
		t.Fatalf("parseBlocks(garbage) = %+v, want nil", blocks)
	}
}
