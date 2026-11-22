package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/trick77/ship/kit/httpx"
)

// pollHold is how long a long poll waits for the turn's next event before it
// answers empty: well under the 30-60 s after which proxies cut a quiet request.
const pollHold = 20 * time.Second

// pollMaxBytes bounds one poll's raw events; the rest comes with the next poll.
const pollMaxBytes = 256 << 10

// pollEvent is one event of a poll answer, as the stream would have sent it.
type pollEvent struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

// pollResponse is one long poll's answer. Next is the cursor for the next
// poll of the same hub; done means the hub closed and everything was sent.
type pollResponse struct {
	Hub    string      `json:"hub"`
	SendID string      `json:"sendId"`
	Next   int         `json:"next"`
	Done   bool        `json:"done"`
	Events []pollEvent `json:"events"`
}

// handlePollStreamMessage follows the user's running turn on the thread in
// whole responses: the client's fallback when a proxy (TLS inspection) buffers
// every response until it ends, so a stream arrives only once the turn is over.
// Without ?hub it starts where a reattach does (replayFrom) and names the hub
// and the send it belongs to; with ?hub and ?after it goes on from that cursor,
// on the running stream or the one just replaced or ended (recent), so a
// client drains a turn to its terminal event. 204: no turn is running, the
// answer is on the thread. 409 hub_gone: the named hub is neither.
func (s *server) handlePollStreamMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	hubID := q.Get("hub")
	hub, sendID, target := s.activeStreams.pollLookup(user.ID, r.PathValue("threadID"), hubID)
	switch target {
	case pollNone:
		w.WriteHeader(http.StatusNoContent)
		return
	case pollGone:
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": "hub_gone"})
		return
	}
	from := hub.replayStart()
	if hubID != "" {
		after, err := strconv.Atoi(q.Get("after"))
		if err != nil || after < 0 {
			httpx.WriteError(w, http.StatusBadRequest, "after must be a cursor")
			return
		}
		from = after
	}
	hold := s.pollHold
	if hold <= 0 {
		hold = pollHold
	}
	events, next, done := hub.wait(r.Context(), from, hold, pollMaxBytes)
	if r.Context().Err() != nil {
		return
	}
	out := pollResponse{Hub: hub.id, SendID: sendID, Next: next, Done: done, Events: make([]pollEvent, len(events))}
	for i, e := range events {
		out.Events[i] = pollEvent{Event: e.Name, Data: e.Data}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
