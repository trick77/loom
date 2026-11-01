package httpapi

import (
	"net/http"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/usage"
	"github.com/trick77/ship/kit/httpx"
)

// usageResponse is the GET /api/me/usage payload: the user's lifetime counters
// plus live (non-counter) user-memory values — how many facts it holds (and
// how many are long-term), when it was last refreshed, and the rolling refresh
// window so the UI can show a refresh ETA without hardcoding it.
type usageResponse struct {
	usage.Totals
	UserFactsCount               int     `json:"userFactsCount"`
	UserFactsLongTerm            int     `json:"userFactsLongTerm"`
	UserMemoryUpdatedAt          *string `json:"userMemoryUpdatedAt"`
	UserMemoryRefreshWindowHours int     `json:"userMemoryRefreshWindowHours"`
	// Other instructions (user-steered directives) budget usage.
	UserDirectivesCount  int `json:"userDirectivesCount"`
	UserDirectivesLength int `json:"userDirectivesLength"`
	UserDirectivesMax    int `json:"userDirectivesMax"`
}

func (s *server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
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
		UserMemoryRefreshWindowHours: int(memoryUserRefreshAge.Hours()),
		UserDirectivesMax:            chat.MaxUserDirectivesTotalLength,
	}
	// Live values, not counters: the fact counts and when the last refresh ran
	// (so the UI can show a refresh ETA).
	if s.thread != nil {
		if facts, err := s.thread.ListActiveUserFacts(r.Context(), user.ID); err == nil {
			resp.UserFactsCount = len(facts)
			for _, f := range facts {
				if f.Durable {
					resp.UserFactsLongTerm++
				}
			}
		}
		if cursor, ok, err := s.thread.GetUserFactCursor(r.Context(), user.ID); err == nil && ok && cursor.LastRefreshAt != nil {
			updated := cursor.LastRefreshAt.UTC().Format(time.RFC3339)
			resp.UserMemoryUpdatedAt = &updated
		}
		if directives, err := s.thread.ListUserDirectives(r.Context(), user.ID); err == nil {
			resp.UserDirectivesCount = len(directives)
			for _, d := range directives {
				resp.UserDirectivesLength += len([]rune(d.Content))
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
