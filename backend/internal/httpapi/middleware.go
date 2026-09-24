package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// recovery converts panics in downstream handlers into JSON 500 responses.
// When the handler had already started its response (an SSE stream, a partial
// download) nothing more is written: a trailing error would corrupt what the
// client holds, and the stream handlers emit their own terminal error event
// (see recoverToStream).
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic recovered", "err", p, "path", logPath(r), "headers_sent", rec.status != 0, "stack", string(debug.Stack()))
				if rec.status == 0 {
					writeJSONError(rec, http.StatusInternalServerError, "internal server error")
				}
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.ResponseWriter.Write(b)
}

// Flush forwards flushes so SSE streaming handlers keep working through the wrapper.
func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying writer for http.ResponseController.
func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// logging logs each request with method, path, status, and duration.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		// Level reflects the outcome so failures stand out instead of drowning in
		// the INFO request stream: 5xx -> ERROR, 4xx -> WARN, otherwise INFO.
		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		slog.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("path", logPath(r)),
			slog.Int("status", rec.status),
			slog.String("dur", time.Since(start).String()),
		)
	})
}

// logPath is the request path as it may appear in a log line. A share id is the
// bearer token of a public share, so the {shareID} segment is redacted; the mux
// sets the path values on the request before the handler runs, and the logging
// wrapper reads them after it returns.
func logPath(r *http.Request) string {
	path := r.URL.Path
	if shareID := r.PathValue("shareID"); shareID != "" {
		path = strings.Replace(path, shareID, "[redacted]", 1)
	}
	return path
}
