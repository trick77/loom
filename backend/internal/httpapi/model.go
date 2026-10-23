package httpapi

import (
	"net/http"

	"github.com/trick77/ship/kit/httpx"
)

// handleModel describes the chat model to the UI: its id, display name and
// context window, all from its llmwire profile. Public:
// shared pages render the context-% segment for signed-out viewers, and the
// facts are the deployment's, not a user's.
func (s *server) handleModel(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.model)
}
