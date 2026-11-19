package main

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/loomuser"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/toolkit/mcpserver"
)

// maxTokenBody bounds PUT /token: one JSON field holding a short token.
const maxTokenBody = 16 << 10

// Codes on a failed PUT or DELETE /token, so loom's settings page can say
// what went wrong: a 422 is codeMalformed (no token at all) or codeRejected
// (Apple refused it); a 502 is errorKindBridge or errorKindUpstream; a 500 is
// sidecar.CodeStorage (/data could not be written).
const (
	codeMalformed = "malformed"
	codeRejected  = "rejected"
)

// codeNoUser: a per-user route was called without the loom user
// (loomuser.Header); loom always sends one.
const codeNoUser = "no_user"

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, /mcp/library (each loom user's
// own library, for the user loom names), /mcp/catalog (search, open to every
// loom user) and the token routes loom's settings page drives for its user.
func newServer(cfg config, svc *service, version string) http.Handler {
	api := http.NewServeMux()
	// Not behind requireUser: loom discovers the tools (server/discover,
	// tools/list) for nobody in particular. Each tool call reads its caller's
	// account and answers an error without one (errNoUser).
	api.Handle("/mcp/library", mcpserver.New(libraryServer(svc, version)))
	api.Handle("/mcp/catalog", mcpserver.New(catalogServer(svc, version)))
	api.Handle("GET /status", requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := loomuser.FromRequest(r)
		if r.URL.Query().Get("check") == "1" {
			httpx.WriteJSON(w, http.StatusOK, svc.check(r.Context(), user))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, svc.status(user))
	})))
	api.Handle("PUT /token", requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if !httpx.DecodeJSON(w, r, &body, maxTokenBody) {
			return
		}
		st, err := svc.SetToken(r.Context(), loomuser.FromRequest(r), body.Token)
		switch {
		case err == nil:
			httpx.WriteJSON(w, http.StatusOK, st)
		case errors.Is(err, errMalformed):
			sidecar.WriteCode(w, http.StatusUnprocessableEntity, err.Error(), codeMalformed)
		case errors.Is(err, errRejected):
			sidecar.WriteCode(w, http.StatusUnprocessableEntity, err.Error(), codeRejected)
		case errors.Is(err, errStore):
			sidecar.WriteCode(w, http.StatusInternalServerError, err.Error(), sidecar.CodeStorage)
		case r.Context().Err() != nil:
			// loom gave up and already told the user; nothing was stored.
			return
		default:
			// The bridge broke (Apple changed the web player) or Apple failed:
			// the code tells loom's page which, the token is not at fault.
			slog.WarnContext(r.Context(), "tunewire: token check failed", "err", err)
			sidecar.WriteCode(w, http.StatusBadGateway, "could not check the token with Apple Music: "+err.Error(), errorKind(err))
		}
	})))
	api.Handle("DELETE /token", requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, err := svc.ClearToken(loomuser.FromRequest(r))
		if err != nil {
			sidecar.WriteCode(w, http.StatusInternalServerError, err.Error(), sidecar.CodeStorage)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, st)
	})))

	return sidecar.Handler(cfg.token, api)
}

// requireUser refuses a per-user route that names no loom user: every
// account is one person's, and a request for nobody must not reach any.
func requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkUser(loomuser.FromRequest(r)) != nil {
			sidecar.WriteCode(w, http.StatusBadRequest, "the loom user is missing", codeNoUser)
			return
		}
		next.ServeHTTP(w, r)
	})
}
