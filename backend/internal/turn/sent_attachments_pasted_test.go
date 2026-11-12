package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

// The labels are one capped line; only a block marked outside names an idea,
// and an idea id is short.
func TestMarshalPastedTextsCleansTheLabels(t *testing.T) {
	raw := MarshalPastedTexts([]chat.MessagePastedText{
		{Text: "x", LineCount: 1, Outside: true, IdeaID: "i1", IdeaTitle: "T", Source: "Tech\n  news " + strings.Repeat("a", 300)},
		{Text: "mine", LineCount: 1, IdeaID: "i2", IdeaTitle: "spoof", Source: "spoof"},
		{Text: "y", LineCount: 1, Outside: true, IdeaID: strings.Repeat("z", 100)},
	})
	var got []chat.MessagePastedText
	if err := json.Unmarshal(raw, &got); err != nil || len(got) != 3 {
		t.Fatalf("%s %v", raw, err)
	}
	if strings.Contains(got[0].Source, "\n") || len([]rune(got[0].Source)) != maxPastedSourceRunes || !got[0].Outside || got[0].IdeaID != "i1" {
		t.Fatalf("outside block %+v", got[0])
	}
	if got[1].IdeaID != "" || got[1].IdeaTitle != "" || got[1].Source != "" {
		t.Fatalf("plain paste kept idea fields: %+v", got[1])
	}
	if got[2].IdeaID != "" || !got[2].Outside {
		t.Fatalf("long id kept: %+v", got[2])
	}
}
