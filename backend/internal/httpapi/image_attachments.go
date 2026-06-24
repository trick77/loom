package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/imagescale"
	"github.com/trick77/loom/internal/llm"
)

const maxImageAttachmentsPerMessage = 5

func (s *server) imageContentParts(ctx context.Context, userID, threadID, text string, artifactIDs []string) ([]llm.MessageContentPart, error) {
	if len(artifactIDs) == 0 {
		return nil, nil
	}
	if s.artifacts == nil {
		return nil, fmt.Errorf("image attachments are not available")
	}
	if len(artifactIDs) > maxImageAttachmentsPerMessage {
		return nil, fmt.Errorf("too many image attachments")
	}
	parts := make([]llm.MessageContentPart, 0, len(artifactIDs)+1)
	for _, artifactID := range artifactIDs {
		item, ok, err := s.artifacts.Get(ctx, userID, artifactID)
		if err != nil {
			return nil, fmt.Errorf("load image attachment: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("image attachment not found")
		}
		if item.ThreadID != "" && item.ThreadID != threadID {
			return nil, fmt.Errorf("image attachment is out of scope")
		}
		// Enforce the same image allowlist as the upload path here too: a re-attached
		// artifact (e.g. a generated image, or any artifact referenced by id) must be
		// an accepted image type, not merely image/*, so an out-of-allowlist format
		// (e.g. image/bmp) can't slip into the model request via the attach path.
		if !allowedImageMIME(item.MIMEType) {
			return nil, fmt.Errorf("attachment is not a supported image type")
		}
		abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
		if err != nil {
			return nil, fmt.Errorf("image attachment path rejected: %w", err)
		}
		// MiMo's OpenAI-compatible image input accepts data URLs, so the request path
		// base64-encodes each upload in memory. This data URL rides on the message
		// that is re-sent on every tool round of the turn, so the bytes matter:
		// DownscaleForModel caps the longest side / recompresses to JPEG first, which
		// turns a multi-MB original into a few hundred KB without losing signal the
		// tiling vision model would use.
		raw, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read image attachment: %w", err)
		}
		encoded, encodedMIME := imagescale.DownscaleForModel(raw, item.MIMEType)
		parts = append(parts, llm.MessageContentPart{
			Type: "image_url",
			ImageURL: &llm.MessageImageURL{
				URL: "data:" + encodedMIME + ";base64," + base64.StdEncoding.EncodeToString(encoded),
			},
		})
	}
	parts = append(parts, llm.MessageContentPart{Type: "text", Text: text})
	return parts, nil
}

// editImageSource carries the original bytes of an uploaded/prior image that is
// forwarded to the image model for direct editing or transformation.
type editImageSource struct {
	Data []byte
	MIME string
}

// loadEditSourceImage reads the original full-resolution bytes of an image
// artifact for direct editing. Unlike imageContentParts (which downscales hard to
// the vision-input budget and so would reintroduce detail loss), this keeps the
// original and only trims to BFL's input envelope. Returns ok=false when the
// artifact is missing, out of scope, or not a supported image type.
func (s *server) loadEditSourceImage(ctx context.Context, userID, threadID, artifactID string) (editImageSource, bool, error) {
	if s.artifacts == nil || strings.TrimSpace(artifactID) == "" {
		return editImageSource{}, false, nil
	}
	item, ok, err := s.artifacts.Get(ctx, userID, artifactID)
	if err != nil {
		return editImageSource{}, false, fmt.Errorf("load edit source image: %w", err)
	}
	if !ok {
		return editImageSource{}, false, nil
	}
	if item.ThreadID != "" && item.ThreadID != threadID {
		return editImageSource{}, false, nil
	}
	if !allowedImageMIME(item.MIMEType) {
		return editImageSource{}, false, nil
	}
	abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
	if err != nil {
		return editImageSource{}, false, fmt.Errorf("edit source image path rejected: %w", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return editImageSource{}, false, fmt.Errorf("read edit source image: %w", err)
	}
	data, mime := imagescale.DownscaleForEditInput(raw, item.MIMEType)
	return editImageSource{Data: data, MIME: mime}, true, nil
}
