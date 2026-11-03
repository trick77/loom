package main

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/mcpserver"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/secret"
)

// maxTokenBody bounds PUT /token: one JSON field holding a short token.
const maxTokenBody = 16 << 10

// Codes on a failed PUT or DELETE /token, so loom's settings page can say
// what went wrong: a 422 is codeMalformed (no token at all) or codeRejected
// (Apple refused it); a 502 is errorKindBridge or errorKindUpstream; a 500 is
// codeStorage (/data could not be written).
const (
	codeMalformed = "malformed"
	codeRejected  = "rejected"
	codeStorage   = "storage"
)

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, /mcp/library (the user's
// playlists, loom offers it to that user only), /mcp/catalog (search, open to
// every loom user) and the token routes loom's settings page drives.
func newServer(cfg config, svc *service, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp/library", mcpserver.New(libraryServer(svc, version)))
	api.Handle("/mcp/catalog", mcpserver.New(catalogServer(svc, version)))
	api.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("check") == "1" {
			httpx.WriteJSON(w, http.StatusOK, svc.check(r.Context()))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, svc.status())
	})
	api.HandleFunc("PUT /token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if !httpx.DecodeJSON(w, r, &body, maxTokenBody) {
			return
		}
		st, err := svc.SetToken(r.Context(), body.Token)
		switch {
		case err == nil:
			httpx.WriteJSON(w, http.StatusOK, st)
		case errors.Is(err, errMalformed):
			httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "code": codeMalformed})
		case errors.Is(err, errRejected):
			httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "code": codeRejected})
		case errors.Is(err, errStore):
			httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error(), "code": codeStorage})
		case r.Context().Err() != nil:
			// loom gave up and already told the user; nothing was stored.
			return
		default:
			// The bridge broke (Apple changed the web player) or Apple failed:
			// the code tells loom's page which, the token is not at fault.
			slog.WarnContext(r.Context(), "tunewire: token check failed", "err", err)
			httpx.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "could not check the token with Apple Music: " + err.Error(), "code": errorKind(err)})
		}
	})
	api.HandleFunc("DELETE /token", func(w http.ResponseWriter, _ *http.Request) {
		st, err := svc.ClearToken()
		if err != nil {
			httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error(), "code": codeStorage})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, st)
	})

	root := http.NewServeMux()
	httpx.Health(root, nil)
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := httpx.Bearer(r); !ok || !secret.Match(tok, cfg.token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		api.ServeHTTP(w, r)
	}))
	return root
}
