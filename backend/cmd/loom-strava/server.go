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

// maxConnectionBody bounds PUT /connection: a code and a scope string.
const maxConnectionBody = 16 << 10

// Codes on a failed route, so loom's settings page can say what went wrong
// without showing the sidecar's own text: 422 codeMalformed (nothing usable
// sent) or codeRejected (Strava refused the code), 502 codeUpstream (Strava
// failed or could not be reached), 500 codeStorage (/data not writable).
const (
	codeMalformed = "malformed"
	codeRejected  = "rejected"
	codeUpstream  = "upstream"
	codeStorage   = "storage"
)

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token and a valid X-Loom-User, the MCP
// endpoint and the connection routes loom's settings page drives. Every
// route acts only on that user's own connection.
func newServer(cfg config, svc *service, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(toolServer(svc, version)))
	api.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		user := loomuser.FromRequest(r)
		if r.URL.Query().Get("check") == "1" {
			httpx.WriteJSON(w, http.StatusOK, svc.check(r.Context(), user))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, svc.status(user))
	})
	api.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		u, err := svc.authorizeURL(r.URL.Query().Get("redirect_uri"), r.URL.Query().Get("state"))
		if err != nil {
			writeCode(w, http.StatusUnprocessableEntity, "authorize needs an absolute http(s) redirect_uri and a state", codeMalformed)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"url": u})
	})
	api.HandleFunc("PUT /connection", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code  string `json:"code"`
			Scope string `json:"scope"`
		}
		if !httpx.DecodeJSON(w, r, &body, maxConnectionBody) {
			return
		}
		st, err := svc.connect(r.Context(), loomuser.FromRequest(r), body.Code, body.Scope)
		switch {
		case err == nil:
			httpx.WriteJSON(w, http.StatusOK, st)
		case errors.Is(err, errMalformed):
			writeCode(w, http.StatusUnprocessableEntity, "no authorization code", codeMalformed)
		case errors.Is(err, errRejected):
			writeCode(w, http.StatusUnprocessableEntity, err.Error(), codeRejected)
		case errors.Is(err, errStore):
			writeCode(w, http.StatusInternalServerError, err.Error(), codeStorage)
		default:
			slog.WarnContext(r.Context(), "strava: code exchange failed", "err", err)
			writeCode(w, http.StatusBadGateway, "could not exchange the code with Strava: "+err.Error(), codeUpstream)
		}
	})
	api.HandleFunc("DELETE /connection", func(w http.ResponseWriter, r *http.Request) {
		st, err := svc.disconnect(r.Context(), loomuser.FromRequest(r))
		if err != nil {
			writeCode(w, http.StatusInternalServerError, err.Error(), codeStorage)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, st)
	})

	return sidecar.Handler(cfg.token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Checked here, once, for every route: the handlers above trust the
		// id as a map key and a file name. /mcp alone may come without one:
		// loom discovers the tools for nobody in particular, and a tool call
		// without a user answers an error result (tools.begin).
		user := loomuser.FromRequest(r)
		if !validUserID(user) && (r.URL.Path != "/mcp" || user != "") {
			writeCode(w, http.StatusBadRequest, "missing or invalid "+loomuser.Header, codeMalformed)
			return
		}
		api.ServeHTTP(w, r)
	}))
}

func writeCode(w http.ResponseWriter, status int, msg, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": msg, "code": code})
}
