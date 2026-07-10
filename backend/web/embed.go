// Package web embeds the built frontend (web/dist) and serves it as a SPA.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// IndexHTML returns the embedded dist/index.html bytes. Callers that need to
// server-render a per-route <head> (e.g. Open Graph meta for share links) template
// this instead of letting the SPA handler serve the file verbatim. ok is false if
// the file is missing (a build error, since dist is embedded at build time).
func IndexHTML() (html []byte, ok bool) {
	b, err := distFS.ReadFile("dist/index.html")
	return b, err == nil
}

// SPAHandler serves the embedded frontend (web/dist) as a SPA.
func SPAHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time; this is a programmer error
	}
	return spaHandler(sub)
}

// spaHandler serves fsys as a SPA: existing regular files are served directly;
// any other path — unknown client-side routes AND directory paths — falls back
// to index.html (so a directory never renders an http.FileServer listing).
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
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
		if info, err := fs.Stat(fsys, trimLeadingSlash(r.URL.Path)); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}
