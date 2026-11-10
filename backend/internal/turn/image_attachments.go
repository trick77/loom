package turn

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/imagescale"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

const maxImageAttachmentsPerMessage = 5

// imageContentParts turns the image artifacts a user attached into the vision
// content parts for the model, followed by the text part. It validates the
// whole list (count, ownership, image type, readable file) and fails on the
// first problem, so callers run it before persisting anything: a rejected
// attachment list is a plain 400, not an orphaned user turn.
func (s *Engine) imageContentParts(ctx context.Context, userID, text string, artifactIDs []string) ([]llm.MessageContentPart, error) {
	parts, _, err := s.ResolveImageAttachments(ctx, userID, text, artifactIDs)
	return parts, err
}

// ResolveImageAttachments is imageContentParts that also hands back the
// looked-up artifacts, so the send path can record the sent attachments
// without a second batch query.
func (s *Engine) ResolveImageAttachments(ctx context.Context, userID, text string, artifactIDs []string) ([]llm.MessageContentPart, map[string]artifact.Artifact, error) {
	if len(artifactIDs) == 0 {
		return nil, nil, nil
	}
	if s.artifacts == nil {
		return nil, nil, fmt.Errorf("image attachments are not available")
	}
	if len(artifactIDs) > maxImageAttachmentsPerMessage {
		return nil, nil, fmt.Errorf("too many image attachments")
	}
	// One user-scoped batch lookup instead of a query per id; ids that don't
	// resolve are simply absent from the map.
	items, err := s.artifacts.GetMany(ctx, userID, artifactIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("load image attachments: %w", err)
	}
	parts := make([]llm.MessageContentPart, 0, len(artifactIDs)+1)
	for _, artifactID := range artifactIDs {
		// GetMany deliberately returns soft-deleted rows (the transcript overlay
		// needs them); an attachment must be a live artifact with bytes on disk.
		item, ok := items[artifactID]
		if !ok || item.Deleted {
			return nil, nil, fmt.Errorf("image attachment not found")
		}
		// No thread-scope check here: s.artifacts.GetMany already user-scopes the lookup,
		// so a forged id pointing at another user's artifact can't resolve. An
		// artifact keeps the thread it was generated/uploaded in, and "Use in thread"
		// deliberately re-references an existing artifact from a *new* thread, so
		// requiring item.ThreadID == threadID would reject every cross-thread reuse
		// (the image would display via ResolveSentAttachments but never reach the
		// model). User ownership is the real boundary; the thread match added nothing.
		// Enforce the same image allowlist as the upload path here too: a re-attached
		// artifact (e.g. a generated image, or any artifact referenced by id) must be
		// an accepted image type, not merely image/*, so an out-of-allowlist format
		// (e.g. image/bmp) can't slip into the model request via the attach path.
		raw, err := s.readImageArtifact(userID, item)
		if err != nil {
			return nil, nil, err
		}
		// OpenAI-compatible image input accepts data URLs, so the request path
		// base64-encodes each upload in memory. This data URL rides on the message
		// that is re-sent on every tool round of the turn, so the bytes matter:
		// DownscaleForModel caps the longest side / recompresses to JPEG first, which
		// turns a multi-MB original into a few hundred KB without losing signal the
		// tiling vision model would use.
		encoded, encodedMIME := imagescale.DownscaleForModel(raw, item.MIMEType)
		parts = append(parts, llm.MessageContentPart{
			Type: "image_url",
			ImageURL: &llm.MessageImageURL{
				URL: "data:" + encodedMIME + ";base64," + base64.StdEncoding.EncodeToString(encoded),
			},
		})
	}
	parts = append(parts, llm.MessageContentPart{Type: "text", Text: text})
	return parts, items, nil
}

// readImageArtifact reads the bytes of an image artifact the caller already
// looked up user-scoped. Only an allowlisted image type is read, so an
// out-of-allowlist format (e.g. image/bmp) never reaches a model, and only
// from under the user's root.
func (s *Engine) readImageArtifact(userID string, item artifact.Artifact) ([]byte, error) {
	// The errors name no attachment (view_image reads through here too, and
	// a model told "attachment" asked the user to attach the file again) and
	// no path: the os and resolve errors name the absolute volume path, which
	// stays in the log.
	if !artifact.AllowedImageMIME(item.MIMEType) {
		return nil, fmt.Errorf("not a supported image type")
	}
	abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
	if err != nil {
		slog.Warn("image path rejected", "artifact_id", item.ID, "err", err)
		return nil, fmt.Errorf("image path rejected")
	}
	raw, err := os.ReadFile(abs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
	if err != nil {
		slog.Warn("image unreadable", "artifact_id", item.ID, "err", err)
		return nil, fmt.Errorf("image is unreadable")
	}
	return raw, nil
}

// maxEditSourceBytes is the largest source image loom forwards to the image
// tool (input_images): the size the contract (docs/imagegen.md) accepts.
const maxEditSourceBytes = mcp.ImagegenMaxInputBytes

// editImageSource is the original bytes of an uploaded or earlier image that
// an edit turn hands the image tool. The tool keeps its proportions (it reads
// them from the bytes, EXIF orientation too).
type editImageSource struct {
	Data []byte
}

// loadEditSourceImage reads the original full-resolution bytes of an image
// artifact for direct editing. Unlike imageContentParts (which downscales hard to
// the vision-input budget and so would reintroduce detail loss), this keeps the
// original and only trims to the image model's input envelope. Returns ok=false when the
// artifact is missing or not a supported image type. Like imageContentParts it
// accepts any image the user owns, whatever thread it came from: "Use in
// thread" re-references an artifact from a new thread, and rejecting it here
// silently dropped the source of the follow-up edit.
func (s *Engine) loadEditSourceImage(ctx context.Context, userID, artifactID string) (editImageSource, bool, error) {
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
	if !artifact.AllowedImageMIME(item.MIMEType) {
		return editImageSource{}, false, nil
	}
	abs, err := artifact.ResolveExisting(s.usersDir, userID, item.VolumeRelPath)
	if err != nil {
		return editImageSource{}, false, fmt.Errorf("edit source image path rejected: %w", err)
	}
	raw, err := os.ReadFile(abs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
	if err != nil {
		return editImageSource{}, false, fmt.Errorf("read edit source image: %w", err)
	}
	data, _ := imagescale.DownscaleForEditInput(raw, item.MIMEType)
	// DownscaleForEditInput is best-effort: an undecodable (e.g. corrupt) image is
	// returned unshrunk and could still exceed the image model's input cap, which Normalized()
	// would later reject as a hard tool error. Skip it here so the turn degrades to
	// prompt-only generation instead of failing outright.
	if len(data) > maxEditSourceBytes {
		return editImageSource{}, false, nil
	}
	return editImageSource{Data: data}, true, nil
}
