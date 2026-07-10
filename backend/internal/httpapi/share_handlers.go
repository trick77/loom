package httpapi

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/web"
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

// handleShareIndex serves the SPA's index.html for a public share URL, injecting
// per-share Open Graph / Twitter-card <meta> so link-preview bots (Slack, iMessage,
// Discord, X, …) render a rich card with the thread title instead of a bare
// "Loom" box. Real browsers receive the same HTML and boot the SPA as usual
// (ui/src/main.tsx routes /share/ to the read-only viewer).
//
// noindex is kept: link unfurl (preview for someone who already holds the link) and
// search indexing are independent concerns, so revealing the title in a card does
// not weaken the anti-indexing stance. On any lookup miss (unknown, disabled, or a
// DB hiccup) we serve the plain index — the SPA shows its own not-found and no title
// leaks; enrichment is best-effort.
func (s *server) handleShareIndex(w http.ResponseWriter, r *http.Request) {
	noindex(w)
	index, ok := web.IndexHTML()
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	share, found := s.lookupPublicShare(r)
	if !found {
		_, _ = w.Write(index)
		return
	}
	// The author lives only in the sanitized snapshot (same shape as
	// handleGetPublicShare); a missing/garbled author just omits the "Shared by …".
	var stored struct {
		Author string `json:"author"`
	}
	_ = json.Unmarshal(share.Snapshot, &stored)

	base := s.shareAbsoluteBase(r)
	page := injectShareMeta(string(index), share.Title, stored.Author,
		base+"/share/"+share.ShareID, base+"/og-card.png")
	_, _ = w.Write([]byte(page))
}

// lookupPublicShare resolves an active public share for handleShareIndex. A nil
// store, a lookup error, a missing share, or a disabled share all resolve to
// (zero, false) so the caller degrades to serving the plain index.
func (s *server) lookupPublicShare(r *http.Request) (chat.Share, bool) {
	if s.thread == nil {
		return chat.Share{}, false
	}
	share, ok, err := s.thread.GetShareByShareID(r.Context(), r.PathValue("shareID"))
	if err != nil || !ok || !share.Shared {
		return chat.Share{}, false
	}
	return share, true
}

// shareAbsoluteBase returns the scheme+host used to build the absolute og:url and
// og:image (Open Graph requires absolute URLs). It prefers the configured PublicURL
// and falls back to the incoming request (honoring X-Forwarded-Proto behind a proxy)
// so dev/self-hosted setups without PublicURL still produce a working card.
func (s *server) shareAbsoluteBase(r *http.Request) string {
	if s.publicURL != "" {
		return strings.TrimRight(s.publicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

// injectShareMeta inserts the OG/Twitter card tags before </head> and rewrites the
// tab <title>. Every dynamic value is HTML-escaped, so a title/author containing
// quotes or angle brackets cannot break out of the attribute or inject markup.
func injectShareMeta(index, title, author, shareURL, imageURL string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Shared conversation"
	}
	desc := "Shared on Loom"
	if a := strings.TrimSpace(author); a != "" {
		desc = "Shared by " + a + " · Loom"
	}
	t := html.EscapeString(title)
	d := html.EscapeString(desc)
	u := html.EscapeString(shareURL)
	img := html.EscapeString(imageURL)

	meta := strings.Join([]string{
		`    <meta property="og:type" content="website" />`,
		`    <meta property="og:site_name" content="Loom" />`,
		`    <meta property="og:title" content="` + t + `" />`,
		`    <meta property="og:description" content="` + d + `" />`,
		`    <meta property="og:url" content="` + u + `" />`,
		`    <meta property="og:image" content="` + img + `" />`,
		`    <meta name="twitter:card" content="summary_large_image" />`,
		`    <meta name="twitter:title" content="` + t + `" />`,
		`    <meta name="twitter:description" content="` + d + `" />`,
		`    <meta name="twitter:image" content="` + img + `" />`,
		"  </head>",
	}, "\n")

	out := strings.Replace(index, "</head>", meta, 1)
	out = strings.Replace(out, "<title>Loom</title>", "<title>"+t+" · Loom</title>", 1)
	return out
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
	// If a share row already exists (active or previously disabled), re-sharing
	// re-enables it and re-freezes the snapshot, keeping the same public link. Only
	// an active, unchanged share would be a true no-op; re-snapshotting is harmless
	// and matches the "Create public link" intent after a Keep-private toggle.
	if existing, has, err := s.thread.GetShareByThreadID(r.Context(), user.ID, threadID); err != nil {
		serverError(w, r, err, "get share failed")
		return
	} else if has {
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
