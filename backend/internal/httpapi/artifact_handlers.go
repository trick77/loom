package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/imagescale"
	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/ship/kit/httpx"
)

const multipartUploadOverheadBytes = 1 << 20

// artifactCacheControl marks artifact bytes as cacheable forever: an artifact's
// file is immutable once created (uploads/generation write a fresh, unique path
// with O_CREATE|O_EXCL and never overwrite), and rename only changes metadata. It
// is "private" because every download is auth-scoped to one user.
const artifactCacheControl = "private, immutable, max-age=31536000"

func (s *server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	opts, err := listArtifactsOptionsFromRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.artifacts.List(r.Context(), user.ID, opts)
	if err != nil {
		serverError(w, r, err, "list artifacts failed")
		return
	}
	response := make([]artifactListItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, artifactListItemResponse{
			ID:              item.ID,
			ThreadID:        item.ThreadID,
			ProjectID:       item.ProjectID,
			DisplayFilename: item.DisplayFilename,
			MIMEType:        item.MIMEType,
			SizeBytes:       item.SizeBytes,
			ModifiedAt:      item.CreatedAt,
			DownloadURL:     item.DownloadURL,
			ThumbnailURL:    item.ThumbnailURL,
			ThreadTitle:     item.ThreadTitle,
		})
	}
	var nextCursor *string
	if limit := artifact.EffectiveArtifactLimit(opts.Limit); len(items) == limit {
		cursor := artifact.EncodeArtifactCursor(items[len(items)-1].Artifact, opts.Sort)
		nextCursor = &cursor
	}
	httpx.WriteJSON(w, http.StatusOK, artifactListResponse{Items: response, NextCursor: nextCursor})
}

// handleArtifactSummary feeds the library's period picker: the months holding
// artifacts and how many predate the default window.
func (s *server) handleArtifactSummary(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	query := r.URL.Query()
	listType, err := listTypeFromQuery(query.Get("type"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	since, err := timeFromQuery(query.Get("since"), "since")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The client's IANA zone: it lists a month by its local bounds, so the
	// months are cut there. Absent is UTC, and so is one the server cannot
	// load (a zone newer than its tzdata, a browser's Etc/Unknown): the client
	// always sends its zone, and a 400 would leave it with no summary at all.
	loc, err := schedule.LoadLocation(query.Get("tz"))
	if err != nil {
		loc = time.UTC
	}
	summary, err := s.artifacts.Summary(r.Context(), user.ID, artifact.SummaryOptions{Type: listType, Since: since, Location: loc})
	if err != nil {
		serverError(w, r, err, "summarize artifacts failed")
		return
	}
	months := make([]artifactMonthResponse, 0, len(summary.Months))
	for _, m := range summary.Months {
		months = append(months, artifactMonthResponse{Month: m.Month, Count: m.Count})
	}
	httpx.WriteJSON(w, http.StatusOK, artifactSummaryResponse{OlderCount: summary.OlderCount, Months: months})
}

func (s *server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	s.serveArtifactDownload(w, r, user.ID, r.PathValue("artifactID"))
}

// serveArtifactDownload streams an owner's artifact file with caching headers. It
// is shared by the authed download handler and the public share-scoped handler
// (which passes the share owner's id after checking the share allowlist), so the
// file-serving logic lives in exactly one place.
func (s *server) serveArtifactDownload(w http.ResponseWriter, r *http.Request, userID, artifactID string) {
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), userID, artifactID)
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeNotFound(w)
		return
	}
	abs, err := artifact.ResolveExisting(s.usersDir, userID, found.VolumeRelPath)
	if err != nil {
		httpx.WriteError(w, http.StatusForbidden, "artifact path rejected")
		return
	}
	file, err := os.Open(abs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
	if os.IsNotExist(err) {
		httpx.WriteError(w, http.StatusGone, "artifact file is missing")
		return
	}
	if err != nil {
		serverError(w, r, err, "read artifact failed")
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", found.MIMEType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+headerSafeFilename(found.DisplayFilename)+`"`)
	// Immutable bytes → let the browser cache aggressively. ServeContent turns the
	// ETag into a 304 on revisit (If-None-Match), so the lightbox/full-resolution
	// path stops re-downloading megabytes on every artifacts visit.
	w.Header().Set("Cache-Control", artifactCacheControl)
	w.Header().Set("ETag", artifactETag(found.ID, found.SizeBytes))
	http.ServeContent(w, r, found.DisplayFilename, found.CreatedAt, file)
}

// artifactETag builds a strong validator from an artifact's immutable identity
// (id + byte size). The variant tag distinguishes the full download from the
// thumbnail so the two cannot collide in a shared cache.
func artifactETag(id string, sizeBytes int64) string {
	return fmt.Sprintf("%q", fmt.Sprintf("art-%s-%d", id, sizeBytes))
}

// artifactThumbnailETag carries the thumbnail version: the same URL served the
// older, smaller thumbnail, and a revalidation of that one must not get a 304.
func artifactThumbnailETag(id string, sizeBytes int64) string {
	return fmt.Sprintf("%q", fmt.Sprintf("thumb-v%s-%s-%d", artifact.ThumbnailVersion, id, sizeBytes))
}

// handleThumbnailArtifact serves a small JPEG preview for a raster image artifact.
// A non-image (or SVG) artifact has no thumbnail and returns 404 so the UI falls
// back to the original/typed icon. The thumbnail is generated eagerly at
// upload/generation time; this handler also lazily generates and persists one on
// first view for artifacts that predate the feature (or whose sidecar went
// missing), so old images speed up the first time they are shown.
func (s *server) handleThumbnailArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	s.serveArtifactThumbnail(w, r, user.ID, r.PathValue("artifactID"))
}

// serveArtifactThumbnail streams an owner's artifact thumbnail (lazily generating
// it for older artifacts). Shared by the authed and public share-scoped handlers.
func (s *server) serveArtifactThumbnail(w http.ResponseWriter, r *http.Request, userID, artifactID string) {
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), userID, artifactID)
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists || !artifact.IsThumbnailableMIME(found.MIMEType) {
		writeNotFound(w)
		return
	}
	abs, legacy, err := s.resolveOrCreateThumbnail(r.Context(), userID, found)
	if err != nil {
		writeNotFound(w)
		return
	}
	file, err := os.Open(abs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
	if err != nil {
		writeNotFound(w)
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	if legacy {
		// The older, smaller thumbnail stands in on the current size's URL:
		// cached as immutable, the browser would keep it for a year. No store
		// and no validator, so the next view asks for the current size again.
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, found.DisplayFilename+".jpg", time.Time{}, file)
		return
	}
	w.Header().Set("Cache-Control", artifactCacheControl)
	w.Header().Set("ETag", artifactThumbnailETag(found.ID, found.SizeBytes))
	http.ServeContent(w, r, found.DisplayFilename+".jpg", found.CreatedAt, file)
}

// thumbnailRegenerations is how many thumbnails are made from their originals
// at once. A size bump leaves every tile on screen asking at once, each decode
// of a large original takes hundreds of MB, and the container has 2 GiB.
const thumbnailRegenerations = 2

// thumbnailSlot waits for a regeneration slot, or gives up when the request
// goes away; release frees it.
func (s *server) thumbnailSlot(ctx context.Context) (release func(), err error) {
	select {
	case s.thumbnailSlots <- struct{}{}:
		return func() { <-s.thumbnailSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// resolveOrCreateThumbnail returns the absolute path of the artifact's sidecar
// thumbnail, generating and persisting it on demand when it is missing. It is the
// lazy-backfill core: a stored relpath of the current size whose file still exists
// is served directly; otherwise (none yet, gone, or an older size) it waits for a
// regeneration slot, takes a current-size file another request wrote meanwhile,
// or decodes the original into a fresh one. The path is recorded (best-effort — a
// failed write of the column still serves this request), an older-size file is
// removed, and the new file's path is returned. If no fresh one can be made, an
// older-size file still serves, flagged legacy. An error means there is no
// thumbnail at all.
func (s *server) resolveOrCreateThumbnail(ctx context.Context, userID string, found artifact.Artifact) (abs string, legacy bool, err error) {
	if artifact.IsCurrentThumbnail(found.VolumeRelPath, found.ThumbnailRelPath) {
		if abs, err := artifact.ResolveThumbnailExisting(s.usersDir, userID, found.ThumbnailRelPath); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				return abs, false, nil
			}
		}
	}
	release, err := s.thumbnailSlot(ctx)
	if err != nil {
		return "", false, err
	}
	defer release()
	// found was read before the wait: the request that held the slot may have
	// written this artifact's thumbnail already.
	thumbRel := artifact.ThumbnailRelPath(found.VolumeRelPath)
	if abs, err := artifact.ResolveThumbnailExisting(s.usersDir, userID, thumbRel); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			s.recordThumbnail(ctx, userID, found, thumbRel)
			return abs, false, nil
		}
	}
	generate := s.thumbnailGenerator
	if generate == nil {
		generate = s.writeThumbnailFromOriginal
	}
	thumbRel, err = generate(userID, found)
	if err != nil {
		if abs, legacyErr := artifact.ResolveLegacyThumbnail(s.usersDir, userID, found.VolumeRelPath); legacyErr == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				return abs, true, nil
			}
		}
		return "", false, err
	}
	s.recordThumbnail(ctx, userID, found, thumbRel)
	abs, err = artifact.ResolveThumbnailExisting(s.usersDir, userID, thumbRel)
	return abs, false, err
}

// recordThumbnail stores a current-size thumbnail's path on the artifact's row
// (best-effort) and removes the older-size file it replaces.
func (s *server) recordThumbnail(ctx context.Context, userID string, found artifact.Artifact, thumbRel string) {
	if found.ThumbnailRelPath != thumbRel {
		if err := s.artifacts.SetThumbnailRelPath(ctx, userID, found.ID, thumbRel); err != nil {
			slog.Warn("persist artifact thumbnail path failed", "artifact_id", found.ID, "err", err)
		}
	}
	artifact.RemoveLegacyThumbnail(s.usersDir, userID, found.VolumeRelPath)
}

// writeThumbnailFromOriginal decodes the artifact's original into a fresh
// current-size thumbnail and returns its relpath.
func (s *server) writeThumbnailFromOriginal(userID string, found artifact.Artifact) (string, error) {
	origAbs, err := artifact.ResolveExisting(s.usersDir, userID, found.VolumeRelPath)
	if err != nil {
		return "", err
	}
	src, err := os.ReadFile(origAbs) //nolint:gosec // path comes from artifact.ResolveExisting, which rejects absolute paths and .. and verifies containment under the user root after symlink resolution
	if err != nil {
		return "", err
	}
	return artifact.WriteThumbnail(s.usersDir, userID, found.VolumeRelPath, src)
}

// handleDeleteArtifact removes an artifact the caller owns: its row and its file
// on disk. It mirrors documents.Service.Delete — the row goes first, then the
// file is removed best-effort (a missing or unresolvable file never fails the
// request). Ownership is enforced via the user-scoped Get, so a foreign or
// unknown id is a 404. Used by the composer's remove-attachment path so an
// uploaded image doesn't outlive its removal as an orphan.
func (s *server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), user.ID, r.PathValue("artifactID"))
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeNotFound(w)
		return
	}
	// An upload behind a knowledge document goes with its document, or the
	// document would stay embedded and keep feeding answers from a deleted file.
	if s.documents != nil {
		deleted, err := s.documents.DeleteForArtifact(r.Context(), user.ID, found.ID)
		if err != nil {
			serverError(w, r, err, "delete document failed")
			return
		}
		if deleted {
			artifact.RemoveThumbnail(s.usersDir, user.ID, found.VolumeRelPath)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	if err := s.artifacts.Delete(r.Context(), user.ID, found.ID); err != nil {
		serverError(w, r, err, "delete artifact failed")
		return
	}
	if abs, err := artifact.ResolveExisting(s.usersDir, user.ID, found.VolumeRelPath); err == nil {
		_ = os.Remove(abs)
	}
	// Best-effort: drop the sidecar thumbnail from the reserved subtree. Derived from
	// the original's relpath, so it is removed whether or not the row recorded a path
	// (lazy backfill may have written one without a stored relpath if the column
	// update failed) and can never target a real artifact file.
	artifact.RemoveThumbnail(s.usersDir, user.ID, found.VolumeRelPath)
	w.WriteHeader(http.StatusNoContent)
}

// handleRenameArtifact changes an artifact's display filename. The new name
// propagates into chat transcripts through the read-time overlay in
// handleGetThread, so no message rows are rewritten here. Ownership is enforced
// via the user-scoped Get (which excludes soft-deleted artifacts), so a foreign,
// unknown, or already-deleted id is a 404.
func (s *server) handleRenameArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	var body renameArtifactRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.DisplayFilename) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "displayFilename is required")
		return
	}
	// Clean the name to the same standard uploads enforce (strip unsafe chars,
	// reject traversal, cap length) so a direct PATCH cannot store something the
	// upload path would reject.
	displayFilename, err := artifact.SanitizeDisplayName(body.DisplayFilename)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid displayFilename")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), user.ID, r.PathValue("artifactID"))
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeNotFound(w)
		return
	}
	// Lock the extension to the artifact's original so a rename can't change the
	// file type the bytes/MIME imply (the UI keeps it fixed; enforce it server-side too).
	if originalExt := filepath.Ext(found.DisplayFilename); originalExt != "" &&
		!strings.EqualFold(filepath.Ext(displayFilename), originalExt) {
		displayFilename = strings.TrimSuffix(displayFilename, filepath.Ext(displayFilename)) + originalExt
	}
	if err := s.artifacts.Rename(r.Context(), user.ID, found.ID, displayFilename); err != nil {
		serverError(w, r, err, "rename artifact failed")
		return
	}
	found.DisplayFilename = displayFilename
	httpx.WriteJSON(w, http.StatusOK, found.Response())
}

func (s *server) handleUploadImageAttachment(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeNotFound(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, artifact.MaxArtifactSizeBytes+multipartUploadOverheadBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if isRequestBodyTooLarge(err) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "upload too large")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()

	// Validate against the explicit image allowlist (PNG/JPG/JPEG/WebP/GIF) rather
	// than a bare image/* prefix: the model only supports these, so rejecting
	// everything else here keeps unsupported formats out of the LLM path entirely.
	canonicalMIME, extension, ok := artifact.AllowedImageFormat(header.Filename)
	if !ok {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported image format")
		return
	}
	mimeType := header.Header.Get("Content-Type")
	if !artifact.AllowedImageMIME(mimeType) {
		mimeType = canonicalMIME
	}
	if imagescale.TooLarge(file) {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "image has too many pixels")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	var projectIDPtr *string
	if projectID := strings.TrimSpace(r.FormValue("projectId")); projectID != "" {
		projectIDPtr = &projectID
	}
	stored, err := artifact.StoreUpload(r.Context(), s.artifacts, artifact.StoreUploadInput{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        strings.TrimSpace(r.FormValue("threadId")),
		ProjectID:       projectIDPtr,
		DisplayFilename: header.Filename,
		Extension:       extension,
		MIMEType:        mimeType,
		Reader:          file,
		ThreadLimit:     documents.MaxChatDocuments,
		// Eager sidecar thumbnail so the artifacts list and composer previews
		// are fast immediately.
		Thumbnail: true,
	})
	if err != nil {
		writeImageUploadError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stored.Artifact.Response())
}

func writeImageUploadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, artifact.ErrThreadUploadLimit) {
		httpx.WriteError(w, http.StatusConflict, "too many attachments in this thread")
		return
	}
	if errors.Is(err, artifact.ErrUploadTooLarge) {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "upload too large")
		return
	}
	var uploadErr *artifact.UploadError
	if !errors.As(err, &uploadErr) {
		serverError(w, r, err, "save upload failed")
		return
	}
	switch uploadErr.Stage {
	case artifact.UploadStageCount:
		serverError(w, r, err, "count image uploads failed")
	case artifact.UploadStageFile:
		httpx.WriteError(w, http.StatusBadRequest, "invalid upload")
	case artifact.UploadStageWrite:
		serverError(w, r, err, "write upload failed")
	default:
		serverError(w, r, err, "save upload failed")
	}
}

func listArtifactsOptionsFromRequest(r *http.Request) (artifact.ListOptions, error) {
	query := r.URL.Query()
	opts := artifact.ListOptions{
		Search: query.Get("search"),
		Sort:   artifact.SortByModified,
		Order:  artifact.SortDesc,
	}
	var err error
	if opts.Type, err = listTypeFromQuery(query.Get("type")); err != nil {
		return artifact.ListOptions{}, err
	}
	if opts.Since, err = timeFromQuery(query.Get("since"), "since"); err != nil {
		return artifact.ListOptions{}, err
	}
	if opts.Until, err = timeFromQuery(query.Get("until"), "until"); err != nil {
		return artifact.ListOptions{}, err
	}
	switch value := query.Get("sort"); value {
	case "", string(artifact.SortByModified):
	case string(artifact.SortByName):
		opts.Sort = artifact.SortByName
	case string(artifact.SortBySize):
		opts.Sort = artifact.SortBySize
	default:
		return artifact.ListOptions{}, fmt.Errorf("invalid sort")
	}
	switch value := query.Get("order"); value {
	case "", string(artifact.SortDesc):
	case string(artifact.SortAsc):
		opts.Order = artifact.SortAsc
	default:
		return artifact.ListOptions{}, fmt.Errorf("invalid order")
	}
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 0 {
			return artifact.ListOptions{}, fmt.Errorf("invalid limit")
		}
		opts.Limit = limit
	}
	opts.Cursor = query.Get("cursor")
	return opts, nil
}

// listTypeFromQuery maps the type param, shared by the list and the summary.
func listTypeFromQuery(value string) (artifact.ListType, error) {
	switch value {
	case "", string(artifact.ListTypeAll):
		return artifact.ListTypeAll, nil
	case string(artifact.ListTypeImages):
		return artifact.ListTypeImages, nil
	case string(artifact.ListTypeFiles):
		return artifact.ListTypeFiles, nil
	}
	return "", fmt.Errorf("invalid type")
}

// timeFromQuery parses an optional RFC 3339 bound; empty is the zero time.
func timeFromQuery(value, name string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func headerSafeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\r", "_")
	filename = strings.ReplaceAll(filename, "\n", "_")
	filename = strings.ReplaceAll(filename, `"`, "_")
	filename = strings.ReplaceAll(filename, `\`, "_")
	return filename
}
