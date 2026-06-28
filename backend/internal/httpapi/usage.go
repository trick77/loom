package httpapi

import (
	"net/http"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/usage"
)

// usageResponse is the GET /api/me/usage payload: the user's lifetime counters
// plus live (non-counter) user-memory facts — current length, when it was last
// refreshed, how much of the message history it has folded in, and the rolling
// refresh window so the UI can show a refresh ETA without hardcoding it.
type usageResponse struct {
	usage.Totals
	UserMemoryLength             int     `json:"userMemoryLength"`
	UserMemoryMax                int     `json:"userMemoryMax"`
	UserMemoryUpdatedAt          *string `json:"userMemoryUpdatedAt"`
	UserMemorySourceMessages     int     `json:"userMemorySourceMessages"`
	UserMemoryTotalMessages      int     `json:"userMemoryTotalMessages"`
	UserMemoryRefreshWindowHours int     `json:"userMemoryRefreshWindowHours"`
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
			serverError(w, r, err, "load usage failed")
			return
		}
		totals = t
	}
	resp := usageResponse{
		Totals:                       totals,
		UserMemoryMax:                chat.MaxUserMemoryLength,
		UserMemoryRefreshWindowHours: int(memoryUserRefreshAge.Hours()),
	}
	// Live values, not counters: the current memory's length (runes), how many
	// messages it was last folded from, when it was last refreshed, and the user's
	// current total message count (so the UI can show coverage + a refresh ETA).
	if s.thread != nil {
		if mem, ok, err := s.thread.GetUserMemory(r.Context(), user.ID); err == nil && ok {
			resp.UserMemoryLength = len([]rune(mem.Content))
			resp.UserMemorySourceMessages = mem.SourceMessageCount
			if mem.UpdatedAt != nil {
				updated := mem.UpdatedAt.UTC().Format(time.RFC3339)
				resp.UserMemoryUpdatedAt = &updated
			}
		}
		if total, err := s.thread.CountUserMessages(r.Context(), user.ID); err == nil {
			resp.UserMemoryTotalMessages = total
		}
	}
	writeJSON(w, resp)
}
