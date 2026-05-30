package httpapi

import (
	"encoding/json"
	"net/http"
)

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}

// handleHealthStream is implemented in Task 6 (sse). Temporary stub so the
// package compiles.
func (s *server) handleHealthStream(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
