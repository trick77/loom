package httpapi

import (
	"net/http"
	"os"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

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
		writeJSONError(w, http.StatusInternalServerError, "load artifact failed")
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
		writeJSONError(w, http.StatusInternalServerError, "read artifact failed")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", found.MIMEType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+headerSafeFilename(found.DisplayFilename)+`"`)
	http.ServeContent(w, r, found.DisplayFilename, found.CreatedAt, file)
}

func headerSafeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\r", "_")
	filename = strings.ReplaceAll(filename, "\n", "_")
	filename = strings.ReplaceAll(filename, `"`, "_")
	filename = strings.ReplaceAll(filename, `\`, "_")
	return filename
}
