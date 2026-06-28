package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// formatShareTime renders a timestamp as RFC3339 for the public/owner JSON.
func formatShareTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// The share snapshot is built by WHITELIST: a shared message carries ONLY the
// fields below. Everything else a Message holds — reasoning, tool calls, activity
// traces, RAG citations (which name source documents), uploaded attachments (the
// private file), token counts, model, duration — is excluded by construction, so
// no private data can leak into a public page and a future Message field cannot
// silently start leaking. This is the core security boundary of the feature.

type shareSnapshot struct {
	Title    string         `json:"title"`
	Author   string         `json:"author"`
	SharedAt string         `json:"sharedAt,omitempty"`
	Messages []shareMessage `json:"messages"`
}

type shareMessage struct {
	ID      string    `json:"id"`
	Role    chat.Role `json:"role"`
	Content string    `json:"content"`
	// Artifacts / ContentBlocks carry GENERATED artifacts only, with their URLs
	// rewritten to the public share-scoped artifact path. Trace blocks are dropped.
	Artifacts     json.RawMessage `json:"artifacts"`
	ContentBlocks json.RawMessage `json:"contentBlocks"`
	// HadAttachment is true when the original message carried an uploaded file. The
	// file itself is never included; the viewer shows a subtle "attachment not
	// shared" marker so an assistant reply that references it still reads sensibly.
	HadAttachment bool   `json:"hadAttachment,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// buildShareSnapshot turns the owner's live messages into the frozen, sanitized
// public blob and returns the allowlist of generated-artifact ids that the public
// artifact endpoints are then permitted to serve. msgs should already have had
// refreshMessageArtifacts applied so renames/deletes are current. shareID is baked
// into the rewritten artifact URLs so the blob is self-contained for the viewer.
func buildShareSnapshot(shareID, title, author string, msgs []chat.Message) (shareSnapshot, []string, error) {
	snap := shareSnapshot{Title: title, Author: author, Messages: make([]shareMessage, 0, len(msgs))}
	seen := map[string]struct{}{}
	var artifactIDs []string
	addID := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		artifactIDs = append(artifactIDs, id)
	}

	for _, msg := range msgs {
		// Role whitelist: only user/assistant turns are ever shared. Tool rows (none
		// are persisted today) and anything else are dropped.
		if msg.Role != chat.RoleUser && msg.Role != chat.RoleAssistant {
			continue
		}

		artifacts, artIDs, err := rewriteArtifactArrayForShare(shareID, msg.Artifacts)
		if err != nil {
			return shareSnapshot{}, nil, fmt.Errorf("sanitize message %s artifacts: %w", msg.ID, err)
		}
		blocks, blockIDs, err := rewriteContentBlocksForShare(shareID, msg.ContentBlocks, msg.Content)
		if err != nil {
			return shareSnapshot{}, nil, fmt.Errorf("sanitize message %s content blocks: %w", msg.ID, err)
		}
		for _, id := range artIDs {
			addID(id)
		}
		for _, id := range blockIDs {
			addID(id)
		}

		hadAttachment := !isEmptyJSON(msg.Attachments)

		sm := shareMessage{
			ID:            msg.ID,
			Role:          msg.Role,
			Content:       msg.Content,
			Artifacts:     artifacts,
			ContentBlocks: blocks,
			HadAttachment: hadAttachment,
			CreatedAt:     formatShareTime(msg.CreatedAt),
		}

		// Skip a message that contributes nothing visible (e.g. an image-only upload
		// whose file we stripped and that left no caption). Keep it if it had an
		// attachment marker so the transcript still notes something was sent.
		if sm.Content == "" && isEmptyJSON(sm.Artifacts) && isEmptyJSON(sm.ContentBlocks) && !hadAttachment {
			continue
		}

		snap.Messages = append(snap.Messages, sm)
	}

	if artifactIDs == nil {
		artifactIDs = []string{}
	}
	return snap, artifactIDs, nil
}

// rewriteArtifactArrayForShare rewrites every artifact object's download/thumbnail
// URL to the public share-scoped path and returns the referenced ids.
func rewriteArtifactArrayForShare(shareID string, raw json.RawMessage) (json.RawMessage, []string, error) {
	if isEmptyJSON(raw) {
		return json.RawMessage("[]"), nil, nil
	}
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, nil, err
	}
	var ids []string
	for _, obj := range objs {
		if id := rewriteArtifactObjectURLs(shareID, obj); id != "" {
			ids = append(ids, id)
		}
	}
	out, err := json.Marshal(objs)
	if err != nil {
		return nil, nil, err
	}
	return out, ids, nil
}

// rewriteContentBlocksForShare drops every trace block, keeps text and artifact
// blocks, and rewrites artifact-block URLs. If sanitization leaves no blocks but
// the message has text, it synthesizes a single text block so the viewer never
// falls back to rebuilding a trace from reasoning/activity (which the snapshot
// does not carry anyway, but this keeps the contract explicit).
func rewriteContentBlocksForShare(shareID string, raw json.RawMessage, content string) (json.RawMessage, []string, error) {
	textBlock := func() (json.RawMessage, error) {
		if strings.TrimSpace(content) == "" {
			return json.RawMessage("[]"), nil
		}
		return json.Marshal([]map[string]string{{"type": "text", "content": content}})
	}

	if isEmptyJSON(raw) {
		out, err := textBlock()
		return out, nil, err
	}

	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, nil, err
	}
	kept := make([]map[string]json.RawMessage, 0, len(blocks))
	var ids []string
	for _, block := range blocks {
		switch blockType(block) {
		case "trace":
			continue
		case "artifact":
			if artifactRaw, ok := block["artifact"]; ok {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(artifactRaw, &obj); err != nil {
					return nil, nil, err
				}
				if id := rewriteArtifactObjectURLs(shareID, obj); id != "" {
					ids = append(ids, id)
				}
				encoded, err := json.Marshal(obj)
				if err != nil {
					return nil, nil, err
				}
				block["artifact"] = encoded
			}
			kept = append(kept, block)
		default:
			kept = append(kept, block)
		}
	}

	if len(kept) == 0 {
		out, err := textBlock()
		return out, ids, err
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return nil, nil, err
	}
	return out, ids, nil
}

// rewriteArtifactObjectURLs points an embedded artifact's download/thumbnail URLs
// at the absolute public share-scoped path and returns its id. The authed path is
// /api/artifacts/{id}/{download,thumbnail}; the public path baked here is
// /api/shares/{shareID}/artifacts/{id}/{download,thumbnail}, which a logged-out
// viewer can fetch and which the public handlers gate on the share's allowlist.
func rewriteArtifactObjectURLs(shareID string, obj map[string]json.RawMessage) string {
	id := artifactObjectID(obj)
	if id == "" {
		return ""
	}
	base := "/api/shares/" + shareID + "/artifacts/" + id
	if encoded, err := json.Marshal(base + "/download"); err == nil {
		obj["downloadUrl"] = encoded
	}
	// Only rewrite a thumbnail URL the artifact actually had (raster images); leave
	// SVGs/files without one so the viewer falls back to the download/typed icon.
	if _, ok := obj["thumbnailUrl"]; ok {
		if encoded, err := json.Marshal(base + "/thumbnail"); err == nil {
			obj["thumbnailUrl"] = encoded
		}
	}
	return id
}
