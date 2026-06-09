// Package sse provides a minimal Server-Sent Events writer.
package sse

import (
	"fmt"
	"net/http"
	"sync"
)

// Writer streams SSE events to an http.ResponseWriter. Send is safe for
// concurrent use: the main assistant loop and background workers (e.g.
// reasoning-title generation) may emit events at the same time.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

// NewWriter sets SSE headers and returns a Writer, or an error if the
// ResponseWriter does not support flushing.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &Writer{w: w, flusher: flusher}, nil
}

// Send writes one event with the given name and data payload, then flushes.
func (s *Writer) Send(event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
