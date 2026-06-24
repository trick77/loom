package httpapi

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/trick77/loom/internal/chat"
)

func (s *server) imageArtifactRequired(content string, priorMessages []chat.Message) bool {
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
	if !priorConversationHasImageArtifact(priorMessages) {
		return false
	}
	return isImageFollowUpRequest(tokens)
}

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
// This is deliberately STRICTER than imageArtifactRequired's follow-up branch.
// That branch fires on any style word so a styled request still routes to image
// generation, but a bare style word describes a NEW image as readily as an edit:
// "draw a retro car" or "create a neon sign" should produce a fresh image, not a
// recolour of whatever was generated earlier in the thread. So reusing the prior
// image requires (a) not being a creation request, and (b) an explicit edit
// signal — a transform verb/noun ("change", "variation", "version") or a pronoun
// pointing back at the existing image ("make IT cyberpunk") — never a style
// adjective alone.
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
//     "darker", "watercolor") near a back-reference PRONOUN ("make IT BIGGER",
//     "turn IT into a WATERCOLOR"). A target requires a pronoun, never a bare verb,
//     so "make the FONT BIGGER" or "set the background color to blue" (code, no
//     pronoun pointing at the image) do not misfire.
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
	return nearbyTokensEitherOrder(tokens, imageEditTargets, imageBackrefPronouns, imageEditPairDistance)
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
	"restyle": true, "recolor": true, "recolour": true, "upscale": true, "crop": true,
	"variation": true, "variations": true, "variant": true, "variants": true,
	"wandle": true, "bearbeite": true, "variante": true, "zuschneide": true,
}

// imageStyleDescriptors name a distinctive visual style/treatment that rarely shows
// up in ordinary coding chat. Near a pronoun OR an action verb they signal an edit
// ("make IT cyberpunk", "ändere den STIL"). They do NOT route on their own — a bare
// "a minimal style for my code" must not reach the image tool.
var imageStyleDescriptors = map[string]bool{
	"cyberpunk": true, "retro": true, "minimal": true, "minimalist": true, "minimalistisch": true,
	"glitch": true, "neon": true, "stil": true,
}

// imageEditTargets are generic "what to change" words — sizes, brightness, and
// mediums. They occur in ordinary (especially front-end) chat, so they corroborate
// an edit only when near a back-reference PRONOUN pointing at the image ("make IT
// BIGGER", "turn IT into a WATERCOLOR") — never via a bare action verb, so "make
// the FONT bigger" or "set the background color to blue" do not misfire. Colours
// and CSS-ish parts (background, border, shadow, contrast) are intentionally absent:
// they collide head-on with code/CSS chat. The list is common-case, not exhaustive
// — lexical edit detection cannot enumerate every phrasing.
var imageEditTargets = map[string]bool{
	// Size / scale.
	"bigger": true, "smaller": true, "larger": true, "taller": true, "wider": true, "narrower": true,
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
// ..."). They corroborate either a style descriptor or a generic edit-target.
var imageBackrefPronouns = map[string]bool{
	"it": true, "its": true, "this": true, "that": true, "these": true, "those": true, "them": true,
	"es": true, "das": true, "dies": true, "dieses": true, "diese": true, "ihn": true,
}

// imageStyleCorroborators = action verbs + back-reference pronouns: either is
// enough to turn a distinctive style descriptor into an edit signal.
var imageStyleCorroborators = mergeTokenSets(imageEditActions, imageBackrefPronouns)

func mergeTokenSets(sets ...map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for _, set := range sets {
		for token := range set {
			merged[token] = true
		}
	}
	return merged
}
