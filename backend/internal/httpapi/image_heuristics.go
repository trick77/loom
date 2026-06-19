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
// an existing image: a transform verb/noun, or a pronoun standing in for the prior
// image. A standalone style descriptor ("retro", "neon", "cyberpunk") does NOT
// count — it equally describes a brand-new image.
func mentionsImageEditIntent(tokens []string) bool {
	editIntent := map[string]bool{
		"change": true, "turn": true, "restyle": true, "recolor": true, "recolour": true, "edit": true,
		"variation": true, "variations": true, "variant": true, "variants": true, "version": true,
		"aendere": true, "ändere": true, "wandle": true, "bearbeite": true, "variante": true,
		// Pronouns referring back to the existing image ("make IT cyberpunk", "mach ES ...").
		"it": true, "its": true, "this": true, "that": true, "these": true, "those": true, "them": true,
		"es": true, "das": true, "dies": true, "dieses": true, "diese": true, "ihn": true,
	}
	for _, token := range tokens {
		if editIntent[token] {
			return true
		}
	}
	return false
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

func isImageFollowUpRequest(tokens []string) bool {
	terms := map[string]bool{
		"make": true, "turn": true, "change": true, "try": true, "restyle": true, "variation": true, "variant": true, "version": true, "style": true,
		"cyberpunk": true, "retro": true, "minimal": true, "minimalist": true, "colors": true, "colour": true, "glitch": true, "neon": true,
		"mach": true, "mache": true, "machen": true, "aendere": true, "ändere": true, "wandle": true, "probiere": true,
		"variante": true, "stil": true, "minimalistisch": true, "farben": true,
	}
	for _, token := range tokens {
		if terms[token] {
			return true
		}
	}
	return false
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
