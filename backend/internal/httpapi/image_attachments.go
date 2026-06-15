package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/llm"
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
		if !strings.HasPrefix(item.MIMEType, "image/") {
			return nil, fmt.Errorf("attachment is not an image")
		}
		abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
		if err != nil {
			return nil, fmt.Errorf("image attachment path rejected: %w", err)
		}
		// MiMo's OpenAI-compatible image input accepts data URLs, so the current
		// request path base64-encodes each bounded upload in memory. Note this data
		// URL rides on the message that is re-sent on every tool round of the turn,
		// so a multi-round vision turn re-transmits the image(s) each round. Bounded
		// by maxImageAttachmentsPerMessage × MaxArtifactSizeBytes; switch to a
		// model-fetchable URL instead of an inline data URL if that bound bites.
		bytes, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read image attachment: %w", err)
		}
		parts = append(parts, llm.MessageContentPart{
			Type: "image_url",
			ImageURL: &llm.MessageImageURL{
				URL: "data:" + item.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(bytes),
			},
		})
	}
	parts = append(parts, llm.MessageContentPart{Type: "text", Text: text})
	return parts, nil
}
