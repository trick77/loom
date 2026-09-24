package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
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
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout is 0, which means no limit")
	}
	// ReadTimeout must stay zero. It bounds the whole request including the
	// body, and the same deadline then cancels r.Context(), so it truncates a
	// large upload and kills a long chat turn. Slow loris is already closed by
	// ReadHeaderTimeout.
	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0: it would cancel the request context mid-stream", srv.ReadTimeout)
	}
	// WriteTimeout must stay zero: internal/sse streams text/event-stream
	// responses that a write deadline would truncate.
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0: it would cut off the SSE stream", srv.WriteTimeout)
	}
}

// A listener that fails to bind (port in use, bad address) must fail run():
// logging the error inside the accept goroutine and then waiting for a signal
// leaves a process that looks alive but serves nothing.
func TestServeReturnsListenError(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer taken.Close()

	srv := newServer(taken.Addr().String(), http.NewServeMux())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, func(context.Context) {}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serve() error = nil, want bind failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not return after the listener failed")
	}
}
