package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
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
