package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/llmwire"
)

// ImageIntentAction is the router's read of what a single user turn asks the
// image tool to do.
type ImageIntentAction string

const (
	// ImageIntentNone is the safe default: the turn is not an image request.
	ImageIntentNone ImageIntentAction = "none"
	// ImageIntentCreate asks to generate a brand-new image.
	ImageIntentCreate ImageIntentAction = "create"
	// ImageIntentEdit asks to change/restyle/resize an image that already exists
	// in the conversation or was attached this turn.
	ImageIntentEdit ImageIntentAction = "edit"
)

// ImageIntent is the semantic router's read of a single user turn: whether it
// asks to create or edit an image (or neither), and whether the result must
// render legible text (logo/typography) so a text-capable image model is picked.
//
// It replaces the former hand-maintained English/German keyword lists in httpapi
// (image_heuristics.go, image_typography.go): the helper model reads the user's
// own words in ANY language, so routing no longer depends on enumerating verbs,
// nouns, and inflections per language.
type ImageIntent struct {
	Action    ImageIntentAction
	NeedsText bool
}

// imageIntentMaxCompletionTokens caps the router reply. Bigger than the
// title/classifier budget because the reply is a small JSON object rather than a
// single token; kept tight so a runaway reply cannot burn output. thinking is
// disabled, so no reasoning tokens are spent.
const imageIntentMaxCompletionTokens = 64

// imageIntentSystemPrompt is deliberately language-agnostic: it describes the
// decision by MEANING and lists the coding/writing false positives the old
// lexicon fought (in prose, as examples across languages), instead of matching
// keywords. The reply is strict JSON so the parse below stays trivial.
var imageIntentSystemPrompt = strings.TrimSpace(`
You route one message in a chat with an AI assistant that can ALSO generate and edit images. Decide the user's intent for THIS message and reply with ONLY a compact JSON object — no prose, no code fence:
{"action":"create","needs_text":false}

action:
- "create": the user asks to produce a NEW image, picture, logo, icon, drawing, painting, poster, sticker, infographic, etc. Examples (any language): "draw a fox in snow", "erstelle ein Logo fuer eine Baeckerei", "dessine-moi une affiche retro", "genera un'immagine di un gatto".
- "edit": the user asks to change, restyle, recolor, resize, crop, or make a variation of an image that ALREADY exists in the conversation or that they attached this turn. Examples: "make it bigger", "mach es cyberpunk", "rends-le plus lumineux", "trasformalo in un acquerello". Use "edit" ONLY when an image is present (see the flags in the message).
- "none": anything else.

This assistant is ALSO a coding and writing helper, so DO NOT read these as image intent — answer "none":
- changing code, CSS, or UI: "make the font bigger", "set the background color to blue", "make this div bigger", "mach die Schrift groesser".
- converting or rendering data/text/markup: "render the JSON", "convert this to CSV", "render this template", "wandle das in CSV um".
- figurative language: "draw a conclusion", "paint a picture of the situation".
- asking to WRITE text content (a summary, email, plan, pitch, poem) — that is not an image.

needs_text: true when the image must contain specific, legible text or lettering — a logo, wordmark, monogram, poster, sign, banner, label, menu, certificate, or any request that quotes the words to render (e.g. "a banner that says 'OPEN TODAY'", "ein Plakat mit der Aufschrift 'Sommerfest'"). Otherwise false. Only meaningful when action is "create" or "edit".

Judge intent from meaning in ANY language, never from specific keywords.`)

// ClassifyImageIntent decides whether a single user turn routes to image
// generation/editing and whether it needs a text-capable image model. It always
// returns a usable value — ImageIntent{Action: ImageIntentNone} on any request,
// decode, or empty-reply failure — so callers can use the result
// unconditionally; the returned error is informational (for logging) only.
//
// hasAttachedImage and threadHasImage are passed to the model so it can tell a
// create ("draw a cat") from an edit ("make it bigger") and never label a turn
// "edit" when there is no image to edit.
func (c *Client) ClassifyImageIntent(ctx context.Context, userMessage string, hasAttachedImage, threadHasImage bool) (ImageIntent, error) {
	framed := fmt.Sprintf("Flags: image_attached_this_turn=%t, conversation_already_has_an_image=%t\n\nUser message:\n\"\"\"\n%s\n\"\"\"\n\nJSON:",
		hasAttachedImage, threadHasImage, strings.TrimSpace(userMessage))
	messages := []Message{
		{Role: "system", Content: imageIntentSystemPrompt},
		{Role: "user", Content: framed},
	}
	// Log the decision, not the prompt: what the gate concluded is the one thing
	// needed to tell a mis-route from a correct route in the logs.
	reply, err := c.shortGate(ctx, messages, imageIntentMaxCompletionTokens, func(reply completion) []slog.Attr {
		intent := parseImageIntent(reply.Content)
		return []slog.Attr{slog.String("intent", string(intent.Action)), slog.Bool("needs_text", intent.NeedsText)}
	})
	if err != nil {
		return ImageIntent{Action: ImageIntentNone}, err
	}
	if reply.Empty {
		return ImageIntent{Action: ImageIntentNone}, nil
	}
	return parseImageIntent(reply.Content), nil
}

// parseImageIntent extracts the {"action","needs_text"} object from the model
// reply, tolerating surrounding prose or a code fence, and coerces anything
// unrecognized to a safe ImageIntentNone so a bad reply never routes a turn to
// the image tool by accident.
func parseImageIntent(reply string) ImageIntent {
	raw, ok := llmwire.JSONObject(reply)
	if !ok {
		return ImageIntent{Action: ImageIntentNone}
	}
	var decoded struct {
		Action    string `json:"action"`
		NeedsText bool   `json:"needs_text"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return ImageIntent{Action: ImageIntentNone}
	}
	switch ImageIntentAction(strings.ToLower(strings.TrimSpace(decoded.Action))) {
	case ImageIntentCreate:
		return ImageIntent{Action: ImageIntentCreate, NeedsText: decoded.NeedsText}
	case ImageIntentEdit:
		return ImageIntent{Action: ImageIntentEdit, NeedsText: decoded.NeedsText}
	default:
		return ImageIntent{Action: ImageIntentNone}
	}
}
