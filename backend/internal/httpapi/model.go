package httpapi

import (
	"net/http"

	"github.com/trick77/loom/internal/llm"
)

// handleModel describes the chat model to the UI. Public, like /api/health:
// shared pages render the context-% segment for signed-out viewers, and the
// facts are the build's, not a user's.
func (s *server) handleModel(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"model":         llm.ModelSummary(),
		"contextWindow": llm.ContextWindow(),
	})
}
