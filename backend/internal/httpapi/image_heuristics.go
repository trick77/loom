package httpapi

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/trick77/loom/internal/chat"
)

func (s *server) imageArtifactRequired(content string, hasAttachedImage bool, priorMessages []chat.Message) bool {
	if len(s.imageTools) == 0 || s.artifacts == nil || strings.TrimSpace(s.usersDir) == "" {
		return false
	}
	tokens := wordTokens(content)
	if len(tokens) == 0 {
		return false
	}
	if isImageCreationRequest(tokens) {
		return true
	}
	// A freshly uploaded photo plus a transform/edit instruction ("render a lego
	// set from this photo") routes straight to the image tool. Without this the
	// request carries no object noun ("image"/"bild"), so it would fall through to
	// the normal tool loop where the classifier's writing/general block can steer
	// the model away from generating. Requiring an edit verb (not a bare
	// attachment) keeps "describe this image" / "what's wrong with this code?" out.
	if hasAttachedImage && isAttachedImageEditRequest(tokens) {
		return true
	}
	if !priorConversationHasImageArtifact(priorMessages) {
		return false
	}
	return isImageFollowUpRequest(tokens)
}

// isAttachedImageEditRequest reports whether — given the user attached an image
// this turn — the text reads as a request to edit/transform that image rather than
// merely ask about it. It fires on an inherently image-producing verb on its own
// ("RENDER a lego set", "DRAW this as a cartoon"), on the existing edit-intent
// vocabulary ("make it cyberpunk", "UPSCALE it"), or on a generic transform verb
// paired with a visual target/style noun ("TURN this into a LEGO set"). It does
// NOT fire on a bare question or a text/data conversion ("convert this to CSV"),
// so describe/extract turns stay on the normal chat path.
func isAttachedImageEditRequest(tokens []string) bool {
	if containsAnyToken(tokens, imageOutputVerbs) {
		return true
	}
	if mentionsImageEditIntent(tokens) {
		return true
	}
	return nearbyTokensEitherOrder(tokens, imageTransformVerbs, imageVisualTargets, imageAttachmentEditPairDistance)
}

// imageAttachmentEditPairDistance is the verb↔target window for an explicitly
// attached image. It is wider than imageEditPairDistance because the attachment
// already disambiguates intent (no code/CSS false-positive risk), so a natural
// phrase like "transform this into a marble statue" still pairs.
const imageAttachmentEditPairDistance = 6

func priorConversationHasImageArtifact(messages []chat.Message) bool {
	for _, message := range messages {
		var artifacts []struct {
			MIMEType      string `json:"mimeType"`
			SnakeMIMEType string `json:"mime_type"`
		}
		if err := json.Unmarshal(message.Artifacts, &artifacts); err != nil {
			continue
		}
		for _, item := range artifacts {
			if strings.HasPrefix(item.MIMEType, "image/") || strings.HasPrefix(item.SnakeMIMEType, "image/") {
				return true
			}
		}
	}
	return false
}

// latestImageArtifactID returns the id of the most recent image artifact across
// the conversation, or "" when there is none. Messages arrive oldest-first
// (ORDER BY created_at ASC), so the last image artifact seen is the newest — the
// one a follow-up edit ("make it cyberpunk") should silently reuse as its source.
func latestImageArtifactID(messages []chat.Message) string {
	latest := ""
	for _, message := range messages {
		var artifacts []struct {
			ID            string `json:"id"`
			MIMEType      string `json:"mimeType"`
			SnakeMIMEType string `json:"mime_type"`
		}
		if err := json.Unmarshal(message.Artifacts, &artifacts); err != nil {
			continue
		}
		for _, item := range artifacts {
			if item.ID == "" {
				continue
			}
			if strings.HasPrefix(item.MIMEType, "image/") || strings.HasPrefix(item.SnakeMIMEType, "image/") {
				latest = item.ID
			}
		}
	}
	return latest
}

// isImageEditFollowUp reports whether this turn edits/restyles the conversation's
// existing image ("make it cyberpunk", "create a variation") — gating the silent
// reuse of the prior image as the model's vision input so the user never has to
// re-attach it by hand.
//
// It shares mentionsImageEditIntent with imageArtifactRequired's follow-up branch,
// so the two recognize an edit identically; the only difference is that this gate
// treats a fresh-creation request ("draw a retro car", "create a neon sign") as
// NOT an edit — there is no prior image to silently reuse — whereas
// imageArtifactRequired routes creations to the image tool as well. Reusing the
// prior image therefore requires (a) not being a creation request, (b) a prior
// image to reuse, and (c) a genuine edit signal (see mentionsImageEditIntent) —
// never a bare style adjective or a lone pronoun.
func (s *server) isImageEditFollowUp(content string, priorMessages []chat.Message) bool {
	if len(s.imageTools) == 0 || s.artifacts == nil || strings.TrimSpace(s.usersDir) == "" {
		return false
	}
	tokens := wordTokens(content)
	if len(tokens) == 0 {
		return false
	}
	if isImageCreationRequest(tokens) {
		return false
	}
	if !priorConversationHasImageArtifact(priorMessages) {
		return false
	}
	return mentionsImageEditIntent(tokens)
}

// mentionsImageEditIntent reports whether the prompt explicitly references editing
// an existing image. A single weak token never suffices, because the most common
// edit words are also the most common chat words: a bare back-reference pronoun
// ("what does THIS mean?") or a bare generic verb ("CHANGE the subject") would
// otherwise silently re-feed the prior image as vision input. And because loom is
// also a coding assistant, the vocabulary is kept clear of words that collide with
// everyday code/CSS chat (colours, "background", "border", "version", "edit",
// "set the … to …"). An edit is recognized only by:
//
//   - a strong, image-specific edit word on its own ("create a VARIATION",
//     "CROP it", "RESTYLE", "UPSCALE"); or
//   - a distinctive style descriptor ("cyberpunk", "retro", "neon", "stil") near a
//     pronoun OR an action verb ("make IT CYBERPUNK", "give IT a RETRO look",
//     "ÄNDERE den STIL"); or
//   - a generic edit-target word (a size, brightness, or medium — "bigger",
//     "darker", "watercolor") near a direct-object back-reference pronoun ("make IT
//     BIGGER", "turn IT into a WATERCOLOR"). A target requires an object pronoun
//     (it/its/them/es/ihn), never a bare verb nor the looser demonstratives
//     this/that, so "make the FONT BIGGER", "set the background color to blue", or
//     "make THIS div bigger" (all code, no object pronoun on the image) do not
//     misfire.
//
// A standalone style/target word still does NOT count — it equally describes a
// brand-new image — and a standalone pronoun or generic verb does not either.
func mentionsImageEditIntent(tokens []string) bool {
	if containsAnyToken(tokens, strongImageEditWords) {
		return true
	}
	if nearbyTokensEitherOrder(tokens, imageStyleDescriptors, imageStyleCorroborators, imageEditPairDistance) {
		return true
	}
	return nearbyTokensEitherOrder(tokens, imageEditTargets, imageBackrefObjectPronouns, imageEditPairDistance)
}

func isImageCreationRequest(tokens []string) bool {
	actions := map[string]bool{
		"generate": true, "create": true, "make": true, "draw": true, "render": true, "paint": true,
		"generiere": true, "generieren": true, "erstelle": true, "erstellen": true, "erzeuge": true, "erzeugen": true,
		"zeichne": true, "zeichnen": true, "male": true, "malen": true, "mach": true, "mache": true, "machen": true,
	}
	objects := map[string]bool{
		"image": true, "images": true, "picture": true, "pictures": true, "logo": true, "logos": true,
		"bild": true, "bilder": true,
	}
	return hasNearbyTokens(tokens, actions, objects, 5)
}

// isImageFollowUpRequest reports whether — given a prior image in the thread — this
// turn should route to image generation. It routes exactly when the turn reads as
// an edit (mentionsImageEditIntent). It deliberately no longer fires on a bare
// generic verb ("MAKE sure to cite that", "CHANGE the subject") or a standalone
// style word ("a MINIMAL style for my code"): those shoved unrelated turns at the
// image tool.
func isImageFollowUpRequest(tokens []string) bool {
	return mentionsImageEditIntent(tokens)
}

func hasNearbyTokens(tokens []string, left, right map[string]bool, maxDistance int) bool {
	for i, token := range tokens {
		if !left[token] {
			continue
		}
		for j := i + 1; j < len(tokens) && j <= i+maxDistance; j++ {
			if right[tokens[j]] {
				return true
			}
		}
	}
	return false
}

// nearbyTokensEitherOrder reports whether a token from setA appears within
// maxDistance of a token from setB, in either order — the symmetric companion to
// hasNearbyTokens (which only matches left-before-right).
func nearbyTokensEitherOrder(tokens []string, setA, setB map[string]bool, maxDistance int) bool {
	return hasNearbyTokens(tokens, setA, setB, maxDistance) || hasNearbyTokens(tokens, setB, setA, maxDistance)
}

func containsAnyToken(tokens []string, set map[string]bool) bool {
	for _, token := range tokens {
		if set[token] {
			return true
		}
	}
	return false
}

func wordTokens(content string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}
	for _, r := range strings.ToLower(content) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// imageEditPairDistance is the (small) token window within which two corroborating
// signals must co-occur to read as an image edit. Kept tight so the pair does not
// straddle a clause boundary (tokenization drops punctuation).
const imageEditPairDistance = 4

// strongImageEditWords are image-specific enough to signal an edit on their own.
// Deliberately narrow: words that also pepper coding/chat ("version", "edit",
// "turn", "flip", "mirror", "rotate", "blur", "sharpen") are NOT here — they live
// in imageEditActions and need a cue. "change" likewise is a generic action, so
// "change the subject" does not read as an edit.
var strongImageEditWords = map[string]bool{
	"restyle": true, "recolor": true, "recolour": true, "crop": true,
	"variation": true, "variations": true, "variant": true, "variants": true,
	"wandle": true, "bearbeite": true, "variante": true, "zuschneide": true,
}

// imageStyleDescriptors name a distinctive visual style/treatment that rarely shows
// up in ordinary coding chat. Near a pronoun OR an action verb they signal an edit
// ("make IT cyberpunk", "ändere den STIL"). They do NOT route on their own.
// "minimal"/"minimalist" are intentionally absent — they collide with everyday
// code/design chat ("a minimal change to that function", "a minimal style").
var imageStyleDescriptors = map[string]bool{
	"cyberpunk": true, "retro": true, "glitch": true, "neon": true, "stil": true,
}

// imageEditTargets are generic "what to change" words — sizes, brightness, and
// mediums. They occur in ordinary (especially front-end / devops) chat, so they
// corroborate an edit only when near a direct-object back-reference pronoun pointing
// at the image ("make IT BIGGER", "turn IT into a WATERCOLOR", "UPSCALE it") — never
// via a bare action verb nor a loose demonstrative, so "make the FONT bigger", "set
// the background color to blue", "make THIS div bigger", or "upscale the deployment"
// do not misfire. Colours and CSS-ish parts (background, border, shadow, contrast)
// are intentionally absent: they collide head-on with code/CSS chat. The list is
// common-case, not exhaustive — lexical edit detection cannot enumerate every
// phrasing.
var imageEditTargets = map[string]bool{
	// Size / scale.
	"bigger": true, "smaller": true, "larger": true, "taller": true, "wider": true, "narrower": true, "upscale": true,
	// Brightness / appearance.
	"brighter": true, "darker": true, "lighter": true, "dimmer": true, "grainier": true,
	// Medium / treatment.
	"watercolor": true, "watercolour": true, "sketch": true, "cartoon": true, "anime": true, "photorealistic": true,
	"charcoal": true, "pixelated": true, "vintage": true, "sepia": true, "grayscale": true, "greyscale": true,
	"monochrome": true, "pastel": true,
	// German targets (Swiss orthography: ss, no ß).
	"grösser": true, "groesser": true, "kleiner": true, "heller": true, "dunkler": true,
	"aquarell": true, "skizze": true,
}

// imageEditActions are imperative verbs that, near a distinctive style descriptor,
// confirm an edit ("ÄNDERE den Stil", "make everything CYBERPUNK"). They never fire
// alone, and they do NOT corroborate a generic edit-target (that needs a pronoun).
// Kept narrow and free of CSS-ish verbs ("set", "give", "put", "add", "remove") so
// "set the background color" / "add a red border" stay out.
var imageEditActions = map[string]bool{
	"make": true, "change": true, "try": true, "turn": true, "edit": true, "restyle": true,
	"mach": true, "mache": true, "machen": true, "aendere": true, "ändere": true, "probiere": true,
}

// imageBackrefPronouns stand in for the prior image ("make IT cyberpunk", "mach ES
// ..."). They corroborate a distinctive style descriptor — distinctive enough that
// even a loose demonstrative ("make THIS cyberpunk") is safe.
var imageBackrefPronouns = map[string]bool{
	"it": true, "its": true, "this": true, "that": true, "these": true, "those": true, "them": true,
	"es": true, "das": true, "dies": true, "dieses": true, "diese": true, "ihn": true,
}

// imageBackrefObjectPronouns are the tighter direct-object pronouns that
// unambiguously point at the prior image as a thing being acted on ("make IT
// bigger", "UPSCALE it"). Used to corroborate the generic edit-target words, where
// the looser demonstratives this/that would leak ("make THIS div bigger").
var imageBackrefObjectPronouns = map[string]bool{
	"it": true, "its": true, "them": true, "es": true, "ihn": true,
}

// imageStyleCorroborators = action verbs + back-reference pronouns: either is
// enough to turn a distinctive style descriptor into an edit signal.
var imageStyleCorroborators = mergeTokenSets(imageEditActions, imageBackrefPronouns)

// imageOutputVerbs are unambiguously about producing/altering a picture, so when
// the user has attached a source image they signal an edit on their own
// ("ILLUSTRATE this", "REDRAW it", "ZEICHNE das neu"). Polysemous verbs whose
// dominant sense is non-visual (render an engine, draw a conclusion, sketch a
// plan, paint a picture of the situation) are intentionally NOT here — they live in
// imageTransformVerbs and require a visual target so an attached chart/screenshot
// question does not misfire.
var imageOutputVerbs = map[string]bool{
	"illustrate": true, "redraw": true, "restyle": true, "recolor": true,
	"recolour": true, "repaint": true,
	"rendere": true, "zeichne": true, "male": true, "illustriere": true, "skizziere": true,
}

// imageTransformVerbs reshape an existing thing into something else. They are
// generic enough to also describe text/data conversions ("convert this to CSV") or
// non-visual idioms ("draw a conclusion", "render the JSON"), so they only signal
// an image edit when paired (within imageAttachmentEditPairDistance) with a visual
// target/style noun.
var imageTransformVerbs = map[string]bool{
	"turn": true, "convert": true, "transform": true, "make": true, "change": true,
	"render": true, "draw": true, "paint": true, "sketch": true,
	"verwandle": true, "umwandle": true, "mache": true, "mach": true, "machen": true,
	"aendere": true, "ändere": true,
	// Re-imagine family: polysemous (apply equally to code/plans/text), so they need a
	// nearby visual target just like the verbs above — "rethink this LOGO", "redesign
	// the ICON" route, but "rework this function" / "redesign the architecture" do not.
	"rethink": true, "reimagine": true, "rework": true, "redesign": true, "reinvent": true,
	"überdenke": true, "überarbeite": true, "umgestalte": true, "umgestalten": true,
}

// imageVisualTargets are nouns/styles whose output is inherently a picture, used to
// disambiguate a transform verb as an image edit ("turn this into a LEGO set",
// "make it a watercolor") from a text/data conversion. Superset of the style and
// medium edit-target vocabulary plus common physical-build / art outputs.
var imageVisualTargets = mergeTokenSets(imageStyleDescriptors, imageEditTargets, map[string]bool{
	"lego": true, "painting": true, "drawing": true, "portrait": true, "statue": true,
	"sculpture": true, "figurine": true, "poster": true, "mosaic": true, "mural": true,
	"caricature": true, "comic": true, "origami": true, "gemälde": true, "gemaelde": true,
	"zeichnung": true, "skulptur": true,
	"logo": true, "logos": true, "icon": true, "icons": true,
})

func mergeTokenSets(sets ...map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for _, set := range sets {
		for token := range set {
			merged[token] = true
		}
	}
	return merged
}
