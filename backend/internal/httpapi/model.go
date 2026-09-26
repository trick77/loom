package httpapi

import "net/http"

// handleModel describes the chat model to the UI: its id, display name and
// context window, all from its llmwire profile. Public, like /api/health:
// shared pages render the context-% segment for signed-out viewers, and the
// facts are the deployment's, not a user's.
func (s *server) handleModel(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.model)
}
