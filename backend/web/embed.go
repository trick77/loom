// Package web embeds the built frontend (web/dist) and serves it as a SPA.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/trick77/ship/kit/spa"
)

// dist holds the Vite build, or only .gitkeep in a fresh clone (ship check
// empties it before the UI build; the image builds its own).
//
//go:embed all:dist
var distFS embed.FS

// placeholder is what an unbuilt binary answers on the app's routes, with a
// 503 (kit's spa): a Go-only build says so instead of serving a blank page.
//
//go:embed placeholder.html
var placeholder []byte

func dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time; this is a programmer error
	}
	return sub
}

// IndexHTML returns the embedded index.html shell. Callers that need to
// server-render a per-route <head> (e.g. Open Graph meta for share links) template
// this instead of letting the SPA handler serve the file verbatim. ok is false
// when the UI was not built.
func IndexHTML() (html []byte, ok bool) {
	b, err := fs.ReadFile(dist(), "index.html")
	return b, err == nil
}

// SPAHandler serves the embedded frontend (kit's spa): files as they are,
// /assets/ cached forever, the shell on every other path outside /api,
// revalidated on every load; the placeholder there when the UI was not built.
func SPAHandler() http.Handler { return handler(dist()) }

// retiredIcons are icon paths dist does not ship. Crawlers, chat unfurlers and
// feed readers probe /favicon.ico blindly without parsing HTML, and a stale
// installed PWA still requests the icons it was installed with by their pinned
// paths; the shell fallback would hand them index.html with a 200, an HTML page
// masquerading as an icon. /favicon.svg is here because the master was renamed
// to /icon.svg when loom picked up peeq's icon mechanism; the manifest icons are
// the two rasters that predated it.
var retiredIcons = map[string]bool{
	"/favicon.ico":                  true,
	"/favicon.svg":                  true,
	"/web-app-manifest-192x192.png": true,
	"/web-app-manifest-512x512.png": true,
}

func handler(d fs.FS) http.Handler {
	app := spa.Handler(d, spa.Options{Placeholder: placeholder})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep public share pages out of search engines (a <meta> tag alone misses
		// non-JS crawlers). Mirrors the X-Robots-Tag set by the share API handlers.
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /share/\n"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/share/") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		// A re-added icon still serves: only a path dist lacks is refused.
		if retiredIcons[r.URL.Path] {
			if _, err := fs.Stat(d, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		app.ServeHTTP(w, r)
	})
}
