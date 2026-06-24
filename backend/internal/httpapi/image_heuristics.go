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
// otherwise silently re-feed the prior image as vision input. An edit needs two
// corroborating signals:
//
//   - a strong, image-specific edit word on its own ("create a VARIATION",
//     "CROP it", "TURN it into a watercolor", "RESTYLE"); or
//   - a style descriptor near a pronoun OR an action verb ("make IT CYBERPUNK",
//     "give IT a RETRO look", "ÄNDERE den STIL") — style words are distinctive
//     enough that a pronoun alone corroborates them; or
//   - a generic edit-target word (a size, colour, medium, or part — "bigger",
//     "darker", "background", "watercolor", "blue") near an ACTION verb ("MAKE it
//     BIGGER", "REMOVE the BACKGROUND"). These require a verb, never a bare
//     pronoun, so chat like "what does THIS RED error mean" does not misfire.
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
	return nearbyTokensEitherOrder(tokens, imageEditTargets, imageEditActions, imageEditPairDistance)
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
// turn should route to image generation. It fires on a standalone style descriptor
// ("mach es CYBERPUNK", "ändere den STIL") or on any genuine edit intent
// (mentionsImageEditIntent). It deliberately does NOT fire on bare generic verbs
// like "make"/"change"/"try" anymore: those alone shoved unrelated turns ("MAKE
// sure to cite that", "CHANGE the subject and tell me about Rome") at the image
// tool.
func isImageFollowUpRequest(tokens []string) bool {
	return containsAnyToken(tokens, imageStyleDescriptors) || mentionsImageEditIntent(tokens)
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

// imageEditPairDistance is the token window within which two corroborating signals
// must co-occur to read as an image edit.
const imageEditPairDistance = 6

// strongImageEditWords are image-specific enough to signal an edit on their own.
// "turn" stays here ("turn it into a watercolor"); "change" was demoted to a
// generic action so "change the subject" no longer reads as an edit. The added
// verbs (crop, rotate, flip, mirror, blur, sharpen, upscale + German) almost never
// occur in ordinary chat after an image, so a bare occurrence is a safe edit cue.
var strongImageEditWords = map[string]bool{
	"restyle": true, "recolor": true, "recolour": true, "edit": true,
	"variation": true, "variations": true, "variant": true, "variants": true, "version": true,
	"turn": true, "wandle": true, "bearbeite": true, "variante": true,
	"crop": true, "rotate": true, "flip": true, "mirror": true, "blur": true, "sharpen": true, "upscale": true,
	"zuschneide": true, "drehe": true, "spiegle": true, "spiegele": true,
}

// imageStyleDescriptors name a visual style/treatment. Standalone they route a
// follow-up to image generation; near a pronoun or action verb they signal an edit
// of the existing image. They are distinctive enough that a bare pronoun ("make IT
// cyberpunk") is sufficient corroboration.
var imageStyleDescriptors = map[string]bool{
	"cyberpunk": true, "retro": true, "minimal": true, "minimalist": true, "minimalistisch": true,
	"glitch": true, "neon": true, "colors": true, "colour": true, "farben": true,
	"stil": true, "style": true,
}

// imageEditTargets are generic "what to change" words — sizes, brightness, colours,
// mediums, and image parts. Unlike style descriptors they DO occur in ordinary chat
// ("a red flag", "this dark theme"), so they corroborate an edit only when near an
// action verb ("MAKE it BIGGER", "REMOVE the BACKGROUND"), never via a bare pronoun.
// The list is deliberately common-case, not exhaustive — lexical edit detection
// cannot enumerate every phrasing.
var imageEditTargets = map[string]bool{
	// Size / scale.
	"bigger": true, "smaller": true, "larger": true, "taller": true, "wider": true, "narrower": true, "huge": true, "tiny": true,
	// Brightness / sharpness / appearance.
	"brighter": true, "darker": true, "lighter": true, "dimmer": true, "bolder": true, "sharper": true, "softer": true, "grainier": true,
	"contrast": true, "saturation": true, "saturated": true, "desaturated": true,
	// Medium / treatment nouns.
	"watercolor": true, "watercolour": true, "sketch": true, "cartoon": true, "anime": true, "realistic": true, "photorealistic": true,
	"oil": true, "pencil": true, "charcoal": true, "pixelated": true, "vintage": true, "sepia": true, "grayscale": true, "greyscale": true,
	"monochrome": true, "pastel": true,
	// Image parts.
	"background": true, "foreground": true, "backdrop": true, "border": true, "frame": true, "shadow": true, "lighting": true,
	// Colours.
	"red": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true, "violet": true, "pink": true,
	"black": true, "white": true, "gray": true, "grey": true, "brown": true, "teal": true, "cyan": true, "magenta": true,
	// German targets (Swiss orthography: ss, no ß).
	"grösser": true, "groesser": true, "kleiner": true, "heller": true, "dunkler": true, "schärfer": true, "schaerfer": true,
	"hintergrund": true, "vordergrund": true, "rahmen": true, "schatten": true, "kontrast": true,
	"aquarell": true, "skizze": true, "realistisch": true,
	"rot": true, "blau": true, "grün": true, "gruen": true, "gelb": true, "schwarz": true, "weiss": true, "grau": true,
}

// imageEditActions are imperative verbs that, near a style descriptor or an
// edit-target word, confirm an edit of the prior image. They never fire alone
// ("make sure to cite that" has no cue nearby, so it stays out).
var imageEditActions = map[string]bool{
	"make": true, "change": true, "try": true, "add": true, "remove": true, "delete": true, "replace": true, "erase": true,
	"give": true, "put": true, "set": true,
	"mach": true, "mache": true, "machen": true, "aendere": true, "ändere": true, "probiere": true,
	"füge": true, "fuege": true, "entferne": true, "entfernen": true, "ersetze": true, "setze": true, "gib": true,
}

// imageBackrefPronouns stand in for the prior image ("make IT cyberpunk", "mach ES
// ..."). They corroborate a style descriptor but never a generic edit-target word.
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
