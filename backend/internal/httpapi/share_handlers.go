package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
)

// share_handlers.go implements the public, unauthenticated share viewer endpoints
// and the owner-only create/update/disable/list endpoints. The public read is the
// security boundary: it is NOT user-scoped, so it must treat a disabled share as
// missing and serve only artifacts in the share's allowlist.

func noindex(w http.ResponseWriter) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// handleGetPublicShare returns the frozen snapshot for a public viewer. No auth.
func (s *server) handleGetPublicShare(w http.ResponseWriter, r *http.Request) {
	noindex(w)
	if !requireThreadStore(w, s) {
		return
	}
	share, ok, err := s.thread.GetShareByShareID(r.Context(), r.PathValue("shareID"))
	if err != nil {
		serverError(w, r, err, "get share failed")
		return
	}
	// Uniform 404 for missing OR disabled OR (cascade-)deleted — no existence oracle.
	if !ok || !share.Shared {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	var stored struct {
		Title    string          `json:"title"`
		Author   string          `json:"author"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(share.Snapshot, &stored); err != nil {
		serverError(w, r, err, "decode share snapshot failed")
		return
	}
	if len(stored.Messages) == 0 {
		stored.Messages = json.RawMessage("[]")
	}
	writeJSON(w, publicShareResponse{
		ShareID:  share.ShareID,
		Title:    stored.Title,
		Author:   stored.Author,
		SharedAt: formatShareTime(share.SnapshotAt),
		Messages: stored.Messages,
	})
}

type publicShareResponse struct {
	ShareID  string          `json:"shareId"`
	Title    string          `json:"title"`
	Author   string          `json:"author"`
	SharedAt string          `json:"sharedAt"`
	Messages json.RawMessage `json:"messages"`
}

// handlePublicShareArtifactDownload serves a generated artifact referenced by an
// active share. Authorization = share is enabled AND the id is in the share's
// allowlist; the file is then loaded under the share OWNER's account. No auth.
func (s *server) handlePublicShareArtifactDownload(w http.ResponseWriter, r *http.Request) {
	noindex(w)
	share, artifactID, ok := s.authorizePublicShareArtifact(w, r)
	if !ok {
		return
	}
	s.serveArtifactDownload(w, r, share.UserID, artifactID)
}

func (s *server) handlePublicShareArtifactThumbnail(w http.ResponseWriter, r *http.Request) {
	noindex(w)
	share, artifactID, ok := s.authorizePublicShareArtifact(w, r)
	if !ok {
		return
	}
	s.serveArtifactThumbnail(w, r, share.UserID, artifactID)
}

// authorizePublicShareArtifact resolves the share and verifies the requested
// artifact id is part of its snapshot allowlist. Any failure is a flat 404.
func (s *server) authorizePublicShareArtifact(w http.ResponseWriter, r *http.Request) (chat.Share, string, bool) {
	if !requireThreadStore(w, s) {
		return chat.Share{}, "", false
	}
	share, ok, err := s.thread.GetShareByShareID(r.Context(), r.PathValue("shareID"))
	if err != nil {
		serverError(w, r, err, "get share failed")
		return chat.Share{}, "", false
	}
	artifactID := r.PathValue("artifactID")
	if !ok || !share.Shared || !share.ContainsArtifactID(artifactID) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return chat.Share{}, "", false
	}
	return share, artifactID, true
}

// handleCreateShare creates (or returns the existing) share for a thread the
// caller owns. Idempotent: re-creating an already-shared thread returns its share.
func (s *server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	thread, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if existing, has, err := s.thread.GetShareByThreadID(r.Context(), user.ID, threadID); err != nil {
		serverError(w, r, err, "get share failed")
		return
	} else if has {
		writeJSON(w, s.shareSummaryOf(existing))
		return
	}

	shareID := chat.NewShareID()
	snapshot, artifactIDs, err := s.buildThreadSnapshot(r.Context(), user, threadID, thread.Title, shareID)
	if err != nil {
		serverError(w, r, err, "build share snapshot failed")
		return
	}
	share, err := s.thread.CreateShare(r.Context(), user.ID, chat.CreateShareInput{
		ShareID:     shareID,
		ThreadID:    threadID,
		Title:       thread.Title,
		Snapshot:    snapshot,
		ArtifactIDs: artifactIDs,
	})
	if err != nil {
		serverError(w, r, err, "create share failed")
		return
	}
	writeJSONStatus(w, http.StatusCreated, s.shareSummaryOf(share))
}

// handleUpdateShare re-freezes the snapshot of an existing share (same link).
func (s *server) handleUpdateShare(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	threadID := r.PathValue("threadID")
	thread, found, err := s.thread.GetThread(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get thread failed")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	existing, has, err := s.thread.GetShareByThreadID(r.Context(), user.ID, threadID)
	if err != nil {
		serverError(w, r, err, "get share failed")
		return
	}
	if !has {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	snapshot, artifactIDs, err := s.buildThreadSnapshot(r.Context(), user, threadID, thread.Title, existing.ShareID)
	if err != nil {
		serverError(w, r, err, "build share snapshot failed")
		return
	}
	share, updated, err := s.thread.UpdateShareSnapshot(r.Context(), user.ID, threadID, chat.UpdateShareInput{
		Title:       thread.Title,
		Snapshot:    snapshot,
		ArtifactIDs: artifactIDs,
	})
	if err != nil {
		serverError(w, r, err, "update share failed")
		return
	}
	if !updated {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, s.shareSummaryOf(share))
}

// handleDisableShare turns off the public link (the "Keep private" action). The
// row and snapshot are kept so re-sharing reuses the same link.
func (s *server) handleDisableShare(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	disabled, err := s.thread.SetShareEnabled(r.Context(), user.ID, r.PathValue("threadID"), false)
	if err != nil {
		serverError(w, r, err, "disable share failed")
		return
	}
	if !disabled {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListMyShares lists the caller's shares for the settings dashboard.
func (s *server) handleListMyShares(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	shares, err := s.thread.ListSharesForUser(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list shares failed")
		return
	}
	items := make([]shareListItem, 0, len(shares))
	for _, share := range shares {
		items = append(items, shareListItem{
			shareSummary: s.shareSummaryOf(share),
			ThreadID:     share.ThreadID,
			Title:        share.Title,
		})
	}
	writeJSON(w, shareListResponse{Items: items})
}

type shareListResponse struct {
	Items []shareListItem `json:"items"`
}

type shareListItem struct {
	shareSummary
	ThreadID string `json:"threadId"`
	Title    string `json:"title"`
}

// buildThreadSnapshot loads the thread's messages (with the artifact overlay
// applied so renames/deletes are current) and produces the sanitized snapshot blob
// plus the generated-artifact allowlist.
func (s *server) buildThreadSnapshot(ctx context.Context, user auth.User, threadID, title, shareID string) (json.RawMessage, []string, error) {
	messages, _, err := s.thread.ListMessages(ctx, user.ID, threadID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.refreshMessageArtifacts(ctx, user.ID, messages); err != nil {
		return nil, nil, err
	}
	snap, artifactIDs, err := buildShareSnapshot(shareID, title, shareAuthorName(user), messages)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(snap)
	if err != nil {
		return nil, nil, err
	}
	return encoded, artifactIDs, nil
}

func (s *server) shareSummaryOf(share chat.Share) shareSummary {
	return shareSummary{
		ShareID:    share.ShareID,
		ShareURL:   s.shareURLFor(share.ShareID),
		Shared:     share.Shared,
		SnapshotAt: formatShareTime(share.SnapshotAt),
	}
}

// shareURLFor builds the absolute share link, falling back to a relative path when
// PublicURL is unset (e.g. dev), so the UI always has something to copy.
func (s *server) shareURLFor(shareID string) string {
	if s.publicURL == "" {
		return "/share/" + shareID
	}
	return strings.TrimRight(s.publicURL, "/") + "/share/" + shareID
}

// shareAuthorName is the display name shown as "Shared by …" in the viewer.
func shareAuthorName(user auth.User) string {
	if name := strings.TrimSpace(user.DisplayName); name != "" {
		return name
	}
	return user.Username
}
