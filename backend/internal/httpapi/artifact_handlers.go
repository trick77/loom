package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/documents"
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
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	opts, err := listArtifactsOptionsFromRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		})
	}
	var nextCursor *string
	if limit := artifact.EffectiveArtifactLimit(opts.Limit); len(items) == limit {
		cursor := artifact.EncodeArtifactCursor(items[len(items)-1], opts.Sort)
		nextCursor = &cursor
	}
	writeJSON(w, artifactListResponse{Items: response, NextCursor: nextCursor})
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
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), userID, artifactID)
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	abs, err := artifact.ResolveExisting(s.usersDir, userID, found.VolumeRelPath)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "artifact path rejected")
		return
	}
	file, err := os.Open(abs)
	if os.IsNotExist(err) {
		writeJSONError(w, http.StatusGone, "artifact file is missing")
		return
	}
	if err != nil {
		serverError(w, r, err, "read artifact failed")
		return
	}
	defer file.Close()
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

func artifactThumbnailETag(id string, sizeBytes int64) string {
	return fmt.Sprintf("%q", fmt.Sprintf("thumb-%s-%d", id, sizeBytes))
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
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), userID, artifactID)
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists || !artifact.IsThumbnailableMIME(found.MIMEType) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	abs, err := s.resolveOrCreateThumbnail(r.Context(), userID, found)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	file, err := os.Open(abs)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", artifactCacheControl)
	w.Header().Set("ETag", artifactThumbnailETag(found.ID, found.SizeBytes))
	http.ServeContent(w, r, found.DisplayFilename+".jpg", found.CreatedAt, file)
}

// resolveOrCreateThumbnail returns the absolute path of the artifact's sidecar
// thumbnail, generating and persisting it on demand when it is missing. It is the
// lazy-backfill core: a stored relpath whose file still exists is served directly;
// otherwise the original is decoded into a fresh thumbnail, the path is recorded
// (best-effort — a failed write of the column still serves this request), and the
// new file's path is returned. An error means no thumbnail could be produced.
func (s *server) resolveOrCreateThumbnail(ctx context.Context, userID string, found artifact.Artifact) (string, error) {
	if found.ThumbnailRelPath != "" {
		if abs, err := artifact.ResolveThumbnailExisting(s.usersDir, userID, found.ThumbnailRelPath); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				return abs, nil
			}
		}
	}
	origAbs, err := artifact.ResolveExisting(s.usersDir, userID, found.VolumeRelPath)
	if err != nil {
		return "", err
	}
	src, err := os.ReadFile(origAbs)
	if err != nil {
		return "", err
	}
	thumbRel, err := artifact.WriteThumbnail(s.usersDir, userID, found.VolumeRelPath, src)
	if err != nil {
		return "", err
	}
	if err := s.artifacts.SetThumbnailRelPath(ctx, userID, found.ID, thumbRel); err != nil {
		slog.Warn("persist artifact thumbnail path failed", "artifact_id", found.ID, "err", err)
	}
	return artifact.ResolveThumbnailExisting(s.usersDir, userID, thumbRel)
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
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), user.ID, r.PathValue("artifactID"))
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
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
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	var body renameArtifactRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(body.DisplayFilename) == "" {
		writeJSONError(w, http.StatusBadRequest, "displayFilename is required")
		return
	}
	// Clean the name to the same standard uploads enforce (strip unsafe chars,
	// reject traversal, cap length) so a direct PATCH cannot store something the
	// upload path would reject.
	displayFilename, err := artifact.SanitizeDisplayName(body.DisplayFilename)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid displayFilename")
		return
	}
	found, exists, err := s.artifacts.Get(r.Context(), user.ID, r.PathValue("artifactID"))
	if err != nil {
		serverError(w, r, err, "load artifact failed")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "not found")
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
	writeJSON(w, artifactResponseFromArtifact(found))
}

func (s *server) handleUploadImageAttachment(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.artifacts == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, artifact.MaxArtifactSizeBytes+multipartUploadOverheadBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if isRequestBodyTooLarge(err) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "upload too large")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	// Validate against the explicit image allowlist (PNG/JPG/JPEG/WebP/GIF) rather
	// than a bare image/* prefix: the model only supports these, so rejecting
	// everything else here keeps unsupported formats out of the LLM path entirely.
	canonicalMIME, extension, ok := allowedImageFormat(header.Filename)
	if !ok {
		writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported image format")
		return
	}
	mimeType := header.Header.Get("Content-Type")
	if !allowedImageMIME(mimeType) {
		mimeType = canonicalMIME
	}
	threadID := strings.TrimSpace(r.FormValue("threadId"))
	projectID := strings.TrimSpace(r.FormValue("projectId"))
	if projectID == "" && threadID != "" {
		count, err := s.countThreadUploads(r.Context(), user.ID, threadID)
		if err != nil {
			serverError(w, r, err, "count image uploads failed")
			return
		}
		if count >= documents.MaxChatDocuments {
			writeJSONError(w, http.StatusConflict, "too many attachments in this thread")
			return
		}
	}
	var projectIDPtr *string
	if projectID != "" {
		projectIDPtr = &projectID
	}
	output, out, err := artifact.CreateUploadFile(artifact.UploadRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ProjectID:       projectIDPtr,
		DisplayFilename: header.Filename,
		Extension:       extension,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	size, copyErr := io.Copy(out, io.LimitReader(file, artifact.MaxArtifactSizeBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(output.AbsPath)
		serverError(w, r, fmt.Errorf("copy err: %v, close err: %v", copyErr, closeErr), "write upload failed")
		return
	}
	if size > artifact.MaxArtifactSizeBytes {
		_ = os.Remove(output.AbsPath)
		writeJSONError(w, http.StatusRequestEntityTooLarge, "upload too large")
		return
	}
	// Eagerly generate the sidecar thumbnail (best-effort) so the artifacts list and
	// composer previews are fast immediately; the bytes were just written to disk.
	var thumbnailRelPath string
	if src, rerr := os.ReadFile(output.AbsPath); rerr == nil {
		thumbnailRelPath = generateThumbnailBestEffort(s.usersDir, user.ID, mimeType, src, output.VolumeRelPath)
	}
	created, err := s.artifacts.Create(r.Context(), artifact.CreateInput{
		UserID:           user.ID,
		ThreadID:         threadID,
		ProjectID:        projectIDPtr,
		DisplayFilename:  output.DisplayFilename,
		VolumeRelPath:    output.VolumeRelPath,
		MIMEType:         mimeType,
		SizeBytes:        size,
		Source:           "user_uploaded",
		ThumbnailRelPath: thumbnailRelPath,
	})
	if err != nil {
		_ = os.Remove(output.AbsPath)
		artifact.RemoveThumbnail(s.usersDir, user.ID, output.VolumeRelPath)
		serverError(w, r, err, "save upload failed")
		return
	}
	writeJSON(w, artifactResponseFromArtifact(created))
}

func (s *server) countThreadUploads(ctx context.Context, userID, threadID string) (int, error) {
	items, err := s.artifacts.ListForThread(ctx, userID, threadID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if item.UserID == userID && item.ProjectID == nil && item.Source == "user_uploaded" {
			count++
		}
	}
	return count, nil
}

func artifactResponseFromArtifact(item artifact.Artifact) artifactResponse {
	return artifactResponse{
		ID:              item.ID,
		DisplayFilename: item.DisplayFilename,
		MIMEType:        item.MIMEType,
		SizeBytes:       item.SizeBytes,
		ProjectID:       item.ProjectID,
		DownloadURL:     item.DownloadURL,
		ThumbnailURL:    item.ThumbnailURL,
	}
}

// generateThumbnailBestEffort writes a sidecar thumbnail for a freshly-created
// raster image artifact, returning its volume-relative path (empty for non-raster
// types or on failure). It never propagates an error: a missing thumbnail is
// backfilled lazily by the thumbnail endpoint on first view.
func generateThumbnailBestEffort(usersDir, userID, mimeType string, src []byte, volumeRelPath string) string {
	if !artifact.IsThumbnailableMIME(mimeType) {
		return ""
	}
	thumbRel, err := artifact.WriteThumbnail(usersDir, userID, volumeRelPath, src)
	if err != nil {
		slog.Warn("generate artifact thumbnail failed", "path", volumeRelPath, "err", err)
		return ""
	}
	return thumbRel
}

func listArtifactsOptionsFromRequest(r *http.Request) (artifact.ListOptions, error) {
	query := r.URL.Query()
	opts := artifact.ListOptions{
		Search: query.Get("search"),
		Type:   artifact.ListTypeAll,
		Sort:   artifact.SortByModified,
		Order:  artifact.SortDesc,
	}
	switch value := query.Get("type"); value {
	case "", string(artifact.ListTypeAll):
	case string(artifact.ListTypeImages):
		opts.Type = artifact.ListTypeImages
	case string(artifact.ListTypeFiles):
		opts.Type = artifact.ListTypeFiles
	default:
		return artifact.ListOptions{}, fmt.Errorf("invalid type")
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

func headerSafeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\r", "_")
	filename = strings.ReplaceAll(filename, "\n", "_")
	filename = strings.ReplaceAll(filename, `"`, "_")
	filename = strings.ReplaceAll(filename, `\`, "_")
	return filename
}
