package httpapi

import (
	"log/slog"
	"math"
	"net/http"

	"github.com/trick77/loom/internal/geo"
	"github.com/trick77/loom/internal/location"
	"github.com/trick77/loom/internal/weather"
	"github.com/trick77/ship/kit/httpx"
)

// pointBody is a point as the page sends it.
type pointBody struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

const pointProblem = "latitude and longitude are required, within ±90 and ±180"

func (b pointBody) valid() bool {
	return b.Latitude != nil && b.Longitude != nil && math.Abs(*b.Latitude) <= 90 && math.Abs(*b.Longitude) <= 180
}

// canLocate reports whether the weather server can name a point's town.
func (s *server) canLocate() bool {
	return s.locationOn() && s.mcp.HasTool(weather.ReverseGeocodeTool)
}

// handleLocate is the start page's "use my location": the town at the
// browser's position is kept as where the user is, over the places their
// memory names (stale or wrong, maybe), until they locate again or drop it.
// It is never a fact. The point leaves loom rounded to two decimals (about a
// kilometre): the town is all that is asked, whatever server answers (the
// page rounds it too, so the exact position never leaves the browser), and
// only the town's centre is kept. One lookup per user at a time: the map
// provider's allowance is everyone's.
func (s *server) handleLocate(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if !s.canLocate() {
		httpx.WriteError(w, http.StatusServiceUnavailable, "locating is not configured")
		return
	}
	var body pointBody
	if !httpx.DecodeJSON(w, r, &body, maxLocationBody) {
		return
	}
	if !body.valid() {
		httpx.WriteError(w, http.StatusBadRequest, pointProblem)
		return
	}
	release, ok := s.inflight.tryAcquire("locate:" + user.ID)
	if !ok {
		httpx.WriteError(w, http.StatusConflict, "already locating, try again shortly")
		return
	}
	defer release()
	ctx := r.Context()
	place, credit, err := weather.Locate(ctx, s.mcp, geo.Coarse(*body.Latitude), geo.Coarse(*body.Longitude), weather.Language(user.ResponseLanguage))
	if err != nil {
		// The map provider busy or away: worth a retry, never loom's fault.
		slog.Warn("locate failed", "user_id", user.ID, "err", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "the town could not be named right now, try again shortly")
		return
	}
	if place == nil {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "no town there", "code": "not_found"})
		return
	}
	place.Kind = location.KindLocated
	located := location.Located{Place: *place, Credit: location.Credit{Text: credit.Text, URL: credit.URL}}
	if err := s.locations.SaveLocated(ctx, user.ID, located); err != nil {
		serverError(w, r, err, "save located place failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"place": place, "attribution": credit})
}

// handleDropLocated is "back to my memories": the town located is dropped,
// the places the memory names hold again. It needs no map, so it works when
// locating is off: a town located before must not stay stuck.
func (s *server) handleDropLocated(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.locations == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "the weather is not configured")
		return
	}
	if err := s.locations.ClearLocated(r.Context(), user.ID); err != nil {
		serverError(w, r, err, "drop located place failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
