package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/documents"
)

const multipartUploadOverheadBytes = 1 << 20

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
	abs, err := artifact.ResolveExisting(s.usersDir, user.ID, found.VolumeRelPath)
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
	http.ServeContent(w, r, found.DisplayFilename, found.CreatedAt, file)
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
	w.WriteHeader(http.StatusNoContent)
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
			writeJSONError(w, http.StatusConflict, "too many attachments in this chat")
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
	created, err := s.artifacts.Create(r.Context(), artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        threadID,
		ProjectID:       projectIDPtr,
		DisplayFilename: output.DisplayFilename,
		VolumeRelPath:   output.VolumeRelPath,
		MIMEType:        mimeType,
		SizeBytes:       size,
		Source:          "user_uploaded",
	})
	if err != nil {
		_ = os.Remove(output.AbsPath)
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
	}
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
