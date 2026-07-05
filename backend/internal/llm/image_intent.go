package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
	start := time.Now()
	framed := fmt.Sprintf("Flags: image_attached_this_turn=%t, conversation_already_has_an_image=%t\n\nUser message:\n\"\"\"\n%s\n\"\"\"\n\nJSON:",
		hasAttachedImage, threadHasImage, strings.TrimSpace(userMessage))
	messages := []Message{
		{Role: "system", Content: imageIntentSystemPrompt},
		{Role: "user", Content: framed},
	}
	resp, err := c.executeUtilityChatRequestWithBudget(ctx, messages, imageIntentMaxCompletionTokens)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return ImageIntent{Action: ImageIntentNone}, err
	}
	defer resp.Body.Close()

	var completion chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		err := fmt.Errorf("decode image-intent completion response: %w", err)
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return ImageIntent{Action: ImageIntentNone}, err
	}
	if len(completion.Choices) == 0 {
		observeInference(ctx, c.model, time.Since(start), completion.Usage, "")
		return ImageIntent{Action: ImageIntentNone}, nil
	}
	choice := completion.Choices[0]
	observeInference(ctx, c.model, time.Since(start), completion.Usage, choice.FinishReason)
	return parseImageIntent(choice.Message.Content), nil
}

// parseImageIntent extracts the {"action","needs_text"} object from the model
// reply, tolerating surrounding prose or a code fence, and coerces anything
// unrecognized to a safe ImageIntentNone so a bad reply never routes a turn to
// the image tool by accident.
func parseImageIntent(reply string) ImageIntent {
	raw := extractJSONObject(reply)
	if raw == "" {
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

// extractJSONObject returns the first brace-balanced {...} span in s, or "" when
// there is none. Lets the parse survive a model that wraps the object in a code
// fence or a stray lead-in despite the "ONLY JSON" instruction. Returning the
// first BALANCED object (not first-"{"-to-last-"}") means a reply that echoes the
// prompt's example object before the real one still parses the example rather
// than joining two objects into invalid JSON. Brace counting ignores braces
// inside strings so a "}" in a value does not close early.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
