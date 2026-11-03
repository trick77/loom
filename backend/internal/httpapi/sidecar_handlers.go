package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/ship/kit/httpx"
)

// The settings routes of loom's per-user sidecars (Apple Music, Strava) share
// one shape: only users on the feature's list see it, each acting on their
// own account, and a sidecar failure is answered by code.

// featureAllowed reports whether user may use a per-user sidecar feature: it
// is configured (on) and lists them. An empty list matches nobody.
func featureAllowed(on bool, users []string, user auth.User) bool {
	return on && mcp.UsersAllow(users, turn.MCPPrincipal(user))
}

// listedUser returns the signed-in user when allowed. Anyone else gets 404:
// the section does not exist for them.
func listedUser(w http.ResponseWriter, r *http.Request, on bool, users []string) (auth.User, bool) {
	user, ok := currentUser(w, r)
	if !ok {
		return auth.User{}, false
	}
	if !featureAllowed(on, users, user) {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return auth.User{}, false
	}
	return user, true
}

// writeSidecar answers with v, or maps a sidecar failure to its HTTP status
// (statuses, by code; 503 otherwise) with {"error", "code"}: the page words it
// by code. A sidecar out of reach is 503 "unavailable" with unavailableText.
// No token ever appears in a response or a log line.
func writeSidecar(w http.ResponseWriter, r *http.Request, v any, err error, statuses map[string]int, unavailableText string) {
	var e *sidecar.Error
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, v)
	case errors.As(err, &e):
		status, ok := statuses[e.Code]
		if !ok {
			status = http.StatusServiceUnavailable
		}
		if status >= http.StatusInternalServerError && status != http.StatusBadGateway {
			slog.ErrorContext(r.Context(), "sidecar failed", "err", err)
		}
		httpx.WriteJSON(w, status, map[string]string{"error": e.Reason, "code": e.Code})
	default:
		slog.ErrorContext(r.Context(), "sidecar failed", "err", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": unavailableText, "code": "unavailable"})
	}
}
