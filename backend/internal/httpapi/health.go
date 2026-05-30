package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/trick77/spark/internal/sse"
)

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}

// handleHealthStream emits a few SSE events to exercise the streaming path.
func (s *server) handleHealthStream(w http.ResponseWriter, r *http.Request) {
	stream, err := sse.NewWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := 1; i <= 3; i++ {
		select {
		case <-r.Context().Done():
			return
		default:
			_ = stream.Send("tick", fmt.Sprintf(`{"n":%d}`, i))
		}
	}
}
