package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/trick77/ship/kit/httpx"
)

// middleware wraps the mux in kit's chain: the access log outermost, so a
// panic's line and its 500 carry the request id, then panic recovery. Every
// request logs at info (5xx error, other 4xx warn); the probes are not logged.
// When a handler had already started its response (an SSE stream, a partial
// download), recovery writes nothing more: the stream handlers emit their own
// terminal error event (see recoverToStream).
func middleware(next http.Handler) http.Handler {
	return httpx.Chain(next,
		httpx.AccessLog(nil, httpx.AccessLogOptions{Path: logPath, RoutineLevel: slog.LevelInfo}),
		httpx.RecoverWith(nil, httpx.RecoverOptions{Path: logPath}),
	)
}

// logPath is the request path as it may appear in a log line. A share id is
// the bearer token of a public share, so the segment after "shares" (the API)
// or "share" (the share page) is redacted. By position, not r.PathValue: the
// access log holds the request as it got it, and the mux sets the path values
// on the copy it hands the handler.
func logPath(r *http.Request) string {
	segments := strings.Split(r.URL.Path, "/")
	for i := 1; i < len(segments); i++ {
		if (segments[i-1] == "shares" || segments[i-1] == "share") && segments[i] != "" {
			segments[i] = "[redacted]"
			return strings.Join(segments, "/")
		}
	}
	return r.URL.Path
}
