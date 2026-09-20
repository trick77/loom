package main

import (
	"net/http"
	"testing"
)

// A zero timeout means "no limit", which is the slow-loris exposure a
// zero-value http.Server has: a client that opens a connection and then stalls
// holds a goroutine and a file descriptor indefinitely. Asserting non-zero is
// what stops a later edit from silently reverting this.
func TestNewServerSetsReadTimeouts(t *testing.T) {
	srv := newServer(":9999", http.NewServeMux())

	if srv.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", srv.Addr)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
	if srv.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout is 0, which means no limit")
	}
	if srv.ReadTimeout == 0 {
		t.Error("ReadTimeout is 0, which means no limit")
	}
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout is 0, which means no limit")
	}
	// Headers alone must not be able to spend the whole read budget.
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Errorf("ReadHeaderTimeout %v exceeds ReadTimeout %v", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	// WriteTimeout must stay zero: internal/sse streams text/event-stream
	// responses that a write deadline would truncate.
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0: it would cut off the SSE stream", srv.WriteTimeout)
	}
}
