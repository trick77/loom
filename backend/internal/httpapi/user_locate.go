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

func (b pointBody) point() (weather.Point, bool) {
	if b.Latitude == nil || b.Longitude == nil || math.Abs(*b.Latitude) > 90 || math.Abs(*b.Longitude) > 180 {
		return weather.Point{}, false
	}
	return weather.Point{Latitude: *b.Latitude, Longitude: *b.Longitude}, true
}

// canLocate reports whether the weather server can name a point's town.
func (s *server) canLocate() bool {
	return s.locationOn() && s.mcp.HasTool(weather.ReverseGeocodeTool)
}

// handleLocate is the start page's "use my location": the town at the
// browser's position, for the user to save by name. It saves nothing: a
// position taken on a trip is no home, the user sees the name first. The
// point leaves loom rounded to two decimals (about a kilometre): the town is
// all that is asked, whatever server answers (the page rounds it too, so
// the exact position never leaves the browser). One lookup per user at a
// time: the map provider's allowance is everyone's.
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
	p, ok := body.point()
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, pointProblem)
		return
	}
	release, ok := s.inflight.tryAcquire("locate:" + user.ID)
	if !ok {
		httpx.WriteError(w, http.StatusConflict, "already locating, try again shortly")
		return
	}
	defer release()
	place, credit, err := weather.Locate(r.Context(), s.mcp, geo.Coarse(p.Latitude), geo.Coarse(p.Longitude), weather.Language(user.ResponseLanguage))
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
	place.Kind = location.KindHome
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"place": place, "attribution": credit})
}
