package httpapi

import (
	"net/http"

	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/usage"
)

// usageResponse is the GET /api/me/usage payload: the user's lifetime counters
// plus the live (non-counter) current user-memory length.
type usageResponse struct {
	usage.Totals
	UserMemoryLength int `json:"userMemoryLength"`
	UserMemoryMax    int `json:"userMemoryMax"`
}

func (s *server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var totals usage.Totals
	if s.usage != nil {
		t, err := s.usage.Get(r.Context(), user.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "load usage failed")
			return
		}
		totals = t
	}
	// Live value, not a counter: current length of the user's memory in runes.
	memLen := 0
	if s.chat != nil {
		if mem, _, err := s.chat.GetUserMemory(r.Context(), user.ID); err == nil {
			memLen = len([]rune(mem.Content))
		}
	}
	writeJSON(w, usageResponse{Totals: totals, UserMemoryLength: memLen, UserMemoryMax: chat.MaxUserMemoryLength})
}
