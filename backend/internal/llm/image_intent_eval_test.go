package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestClassifyImageIntent_multilingualEval is the model-backed proof that the
// semantic gate routes image intent by MEANING across languages — the property
// the old English/German keyword lists could not guarantee. It hits a real
// MiMo-compatible endpoint, so it is skipped unless LOOM_IMAGE_INTENT_EVAL_BASEURL
// is set (CI has no model). Run it against a live backend with:
//
//	LOOM_IMAGE_INTENT_EVAL_BASEURL=http://localhost:1234/v1 \
//	LOOM_IMAGE_INTENT_EVAL_APIKEY=... \
//	go test ./internal/llm/ -run multilingualEval -v
//
// Each case pins the (message, attached, threadHasImage) inputs to an expected
// action. Cases are common-case, not exhaustive; a single failure flags a prompt
// regression to investigate, not a hard contract on every phrasing.
func TestClassifyImageIntent_multilingualEval(t *testing.T) {
	baseURL := os.Getenv("LOOM_IMAGE_INTENT_EVAL_BASEURL")
	if baseURL == "" {
		t.Skip("set LOOM_IMAGE_INTENT_EVAL_BASEURL to run the live multilingual eval")
	}
	c := NewClient(Config{BaseURL: baseURL, APIKey: os.Getenv("LOOM_IMAGE_INTENT_EVAL_APIKEY"), Timeout: 30 * time.Second}, nil)

	cases := []struct {
		msg          string
		attached     bool
		threadHasImg bool
		wantAction   ImageIntentAction
		wantText     *bool // nil = don't assert needs_text
	}{
		// --- create, across languages ---
		{msg: "draw a red fox in the snow", wantAction: ImageIntentCreate},
		{msg: "zeichne mir ein Bild von einem Fuchs", wantAction: ImageIntentCreate},
		{msg: "dessine-moi une affiche retro pour un festival", wantAction: ImageIntentCreate},
		{msg: "genera un'immagine di un gatto astronauta", wantAction: ImageIntentCreate},
		// --- create + typography (needs_text), across languages ---
		{msg: "create a logo for a bakery called 'Sonne'", wantAction: ImageIntentCreate, wantText: boolPtr(true)},
		{msg: "erstelle ein Plakat mit der Aufschrift 'Sommerfest'", wantAction: ImageIntentCreate, wantText: boolPtr(true)},
		// --- edit follow-up (prior image in thread) ---
		{msg: "make it bigger", threadHasImg: true, wantAction: ImageIntentEdit},
		{msg: "mach es cyberpunk", threadHasImg: true, wantAction: ImageIntentEdit},
		{msg: "rends-le plus lumineux", threadHasImg: true, wantAction: ImageIntentEdit},
		{msg: "rendilo un acquerello", threadHasImg: true, wantAction: ImageIntentEdit},
		// --- coding / writing / data: must NOT route (the false positives the lexicon fought) ---
		{msg: "make the font bigger", threadHasImg: true, wantAction: ImageIntentNone},
		{msg: "mach die Schrift grösser in meinem CSS", threadHasImg: true, wantAction: ImageIntentNone},
		{msg: "render the JSON in this response as a table", wantAction: ImageIntentNone},
		{msg: "wandle das in CSV um", wantAction: ImageIntentNone},
		{msg: "write me a short summary of this article", wantAction: ImageIntentNone},
		{msg: "schreib mir eine E-Mail an das Team", wantAction: ImageIntentNone},
	}

	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			intent, err := c.ClassifyImageIntent(context.Background(), tc.msg, tc.attached, tc.threadHasImg)
			if err != nil {
				t.Fatalf("ClassifyImageIntent(%q) error = %v", tc.msg, err)
			}
			if intent.Action != tc.wantAction {
				t.Errorf("ClassifyImageIntent(%q) action = %q, want %q (full: %+v)", tc.msg, intent.Action, tc.wantAction, intent)
			}
			if tc.wantText != nil && intent.NeedsText != *tc.wantText {
				t.Errorf("ClassifyImageIntent(%q) needs_text = %v, want %v", tc.msg, intent.NeedsText, *tc.wantText)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
