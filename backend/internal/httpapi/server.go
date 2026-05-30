// Package httpapi builds spark's HTTP handler: JSON/SSE API plus the embedded SPA.
package httpapi

import "net/http"

// Deps are the dependencies needed to build the server. Grows in later phases
// (store, config, services); for Phase 1 only Version and the static handler.
type Deps struct {
	Version string
	Static  http.Handler // serves the embedded SPA; may be nil in tests
}

type server struct {
	version string
}

// New returns the fully wired HTTP handler.
func New(d Deps) http.Handler {
	s := &server{version: d.Version}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/stream", s.handleHealthStream)
	if d.Static != nil {
		mux.Handle("/", d.Static)
	}

	return logging(recovery(mux))
}
