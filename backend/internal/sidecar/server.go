package sidecar

import (
	"net/http"

	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/secret"
)

// Handler is a sidecar's root: kit's health probes (loopback only, the
// container's own healthcheck) and every other route, api, behind the bearer
// token loom shares with it.
func Handler(token string, api http.Handler) http.Handler {
	root := http.NewServeMux()
	httpx.Health(root, nil)
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := httpx.Bearer(r); !ok || !secret.Match(tok, token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		api.ServeHTTP(w, r)
	}))
	return root
}
