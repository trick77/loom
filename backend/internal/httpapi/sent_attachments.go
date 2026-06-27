package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/trick77/loom/internal/chat"
)

// resolveSentAttachments turns the image-artifact and document ids a user sent
// with a message into the persisted MessageAttachment list, so the sent previews
// survive a reload.
//
// Every id is resolved through the user-scoped stores, so a forged id pointing at
// another user's artifact or document can never attach. Documents must also fall
// in the thread's scope (private to this chat, or in its project). Ids that do
// not resolve or are out of scope are skipped — best-effort, mirroring
// documentInlineContext — rather than failing the send. Returns a JSON array,
// "[]" when nothing resolved.
func (s *server) resolveSentAttachments(ctx context.Context, userID string, thread chat.Thread, imageIDs, documentIDs []string) json.RawMessage {
	attachments := make([]chat.MessageAttachment, 0, len(imageIDs)+len(documentIDs))

	if s.artifacts != nil {
		seen := make(map[string]bool)
		for _, id := range imageIDs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			art, ok, err := s.artifacts.Get(ctx, userID, id)
			if err != nil {
				slog.Warn("sent image attachment lookup failed", "artifact_id", id, "err", err)
				continue
			}
			if !ok {
				slog.Warn("sent image attachment not found", "artifact_id", id)
				continue
			}
			attachments = append(attachments, chat.MessageAttachment{
				Kind:         chat.AttachmentKindImage,
				ArtifactID:   art.ID,
				Filename:     art.DisplayFilename,
				MIMEType:     art.MIMEType,
				SizeBytes:    art.SizeBytes,
				DownloadURL:  art.DownloadURL,
				ThumbnailURL: art.ThumbnailURL,
			})
		}
	}

	if s.documents != nil {
		seen := make(map[string]bool)
		for _, id := range documentIDs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			doc, ok, err := s.documents.Get(ctx, userID, id)
			if err != nil {
				slog.Warn("sent document attachment lookup failed", "document_id", id, "err", err)
				continue
			}
			if !ok || !documentInThreadScope(doc.ProjectID, doc.ThreadID, thread) {
				slog.Warn("sent document attachment out of scope", "document_id", id, "thread_id", thread.ID)
				continue
			}
			attachments = append(attachments, chat.MessageAttachment{
				Kind:       chat.AttachmentKindDocument,
				DocumentID: doc.ID,
				Filename:   doc.Filename,
				MIMEType:   doc.MIME,
				SizeBytes:  doc.SizeBytes,
			})
		}
	}

	if len(attachments) == 0 {
		return json.RawMessage("[]")
	}
	encoded, err := json.Marshal(attachments)
	if err != nil {
		slog.Warn("encode sent attachments failed", "err", err)
		return json.RawMessage("[]")
	}
	return encoded
}
