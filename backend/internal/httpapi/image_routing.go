package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// imageRouting is this turn's image decision, derived from the semantic intent
// gate (llm.ClassifyImageIntent) instead of the former English/German keyword
// lists. Because the gate reads the user's own words in any language, routing is
// language-agnostic — a German, French, or Italian request routes the same as an
// English one.
type imageRouting struct {
	// generate routes the turn to the image tool: a create request, or an edit
	// with a source image available. Was the imageArtifactRequired bool.
	generate bool
	// reuseSource silently reuses the conversation's most recent image as the
	// model's vision input for a follow-up edit ("make it cyberpunk") when the
	// user attached nothing this turn. Was isImageEditFollowUp.
	reuseSource bool
	// typography picks the text-capable image model (FLUX.2 flex) for logo/text
	// work. Was isTypographyImageRequest, run at dispatch on the compiled prompt.
	typography bool
}

// turnGateTimeout bounds the small helper calls a turn blocks on before the
// answer can start: the image-intent gate and the drift classifier. Both are
// 32-64 token routing decisions, and both are fail-safe, so giving up is strictly
// better than making the user wait — a degraded endpoint was observed taking 78s
// on an image_intent call (prompt 99% cache-hit, so it was queueing, not work),
// and nothing bounded it: the turn context has no deadline (see the stream
// handler) and the client only applies its timeout on the streaming path.
//
// A var, not a const, so tests can shorten it.
var turnGateTimeout = 30 * time.Second

// classifyImageTurn decides how this turn routes to image generation/editing. It
// short-circuits (no LLM call) when image tooling is not configured or the turn
// carries no text, then asks the semantic gate and maps its answer with
// imageRoutingFor. The gate is fail-safe (ImageIntentNone on any error), so a
// classification failure — including the turnGateTimeout expiring — degrades to
// the normal, non-image path instead of holding up the answer.
func (s *server) classifyImageTurn(ctx context.Context, user auth.User, threadID, content string, hasAttachedImage bool, priorMessages []chat.Message) imageRouting {
	if len(s.imageTools) == 0 || s.artifacts == nil || strings.TrimSpace(s.usersDir) == "" {
		return imageRouting{}
	}
	// An empty message never routes to the image tool (matching the old
	// token-count guard): a bare attachment with no instruction has nothing to do.
	if strings.TrimSpace(content) == "" {
		return imageRouting{}
	}
	threadHasImage := priorConversationHasImageArtifact(priorMessages)
	meta := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, ThreadID: threadID, Purpose: "image_intent", Round: 1}
	gateCtx, cancel := context.WithTimeout(ctx, turnGateTimeout)
	defer cancel()
	started := time.Now()
	intent, err := s.llm.ClassifyImageIntent(llm.WithInferenceMetadata(gateCtx, meta), content, hasAttachedImage, threadHasImage)
	if err != nil {
		// Log the deadline case distinctly: it means an image request was silently
		// answered as text, which looks like a routing bug from the outside.
		if errors.Is(gateCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			slog.Warn("image-intent gate timed out; routing as non-image",
				"thread_id", threadID, "waited_ms", time.Since(started).Milliseconds(), "timeout", turnGateTimeout)
		} else {
			slog.Warn("image-intent classification failed; routing as non-image", "thread_id", threadID, "err", err)
		}
	}
	return imageRoutingFor(intent, hasAttachedImage, threadHasImage)
}

// imageRoutingFor maps a semantic intent plus the two image-presence flags to the
// concrete routing decision. Pure and language-agnostic, so it is unit-tested
// directly; the language understanding it depends on lives in the gate's prompt.
//
//   - create  -> generate a new image (typography follows needs_text).
//   - edit    -> route to the image tool only when a source image exists (the
//     attachment this turn or a prior generated image); reuse the prior image as
//     the edit source only when nothing was attached this turn.
//   - none    -> the normal chat/tool path.
func imageRoutingFor(intent llm.ImageIntent, hasAttachedImage, threadHasImage bool) imageRouting {
	switch intent.Action {
	case llm.ImageIntentCreate:
		return imageRouting{generate: true, typography: intent.NeedsText}
	case llm.ImageIntentEdit:
		hasSource := hasAttachedImage || threadHasImage
		return imageRouting{
			generate:    hasSource,
			reuseSource: !hasAttachedImage && threadHasImage,
			typography:  hasSource && intent.NeedsText,
		}
	default:
		return imageRouting{}
	}
}

// wordTokens lowercases content and splits it into alphanumeric tokens
// (Unicode-aware, so umlauts survive). Used by the typography fallback below,
// which scans the model-authored compiled image prompt.
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

func containsAnyToken(tokens []string, set map[string]bool) bool {
	for _, token := range tokens {
		if set[token] {
			return true
		}
	}
	return false
}

// priorConversationHasImageArtifact reports whether any earlier message in the
// thread carries an image artifact — the precondition for silently reusing it as
// an edit source.
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
