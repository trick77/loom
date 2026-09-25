package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/httpapi"
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

// A listener that fails must fail run(): logging the error inside the accept
// goroutine and then waiting for a signal leaves a process that looks alive
// but serves nothing.
func TestServeReturnsListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close() // Serve on a closed listener fails at once.

	srv := newServer(ln.Addr().String(), http.NewServeMux())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, ln, httpapi.NewBackground(ctx), func(context.Context) {}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serve() error = nil, want listener failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not return after the listener failed")
	}
}

// Shutdown order matters because the database closes right after serve()
// returns: in-flight requests are told to stop (their context is canceled with
// a shutdown cause, so a stream unwinds and persists its partial answer), the
// listener drains, and then the background group is waited for.
func TestServeShutdownCancelsRequestsThenDrainsBackground(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requestStarted := make(chan struct{})
	requestCause := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hang", func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		requestCause <- context.Cause(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	srv := newServer(ln.Addr().String(), mux)

	ctx, cancel := context.WithCancel(context.Background())
	bg := httpapi.NewBackground(context.Background())
	// The task represents an in-flight refresh: it finishes on its own shortly
	// after the shutdown begins and must be allowed to, not cancelled.
	var backgroundFinished, backgroundCancelled atomic.Bool
	backgroundRelease := make(chan struct{})
	bg.Spawn(context.Background(), "drain", func(ctx context.Context) {
		<-backgroundRelease
		time.Sleep(50 * time.Millisecond)
		backgroundCancelled.Store(ctx.Err() != nil)
		backgroundFinished.Store(true)
	})

	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, bg, func(context.Context) {}) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/hang")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("request never reached the handler")
	}

	cancel()
	close(backgroundRelease)

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after the signal")
	}
	if !errors.Is(<-requestCause, errServerShuttingDown) {
		t.Fatal("request context was not canceled with the shutdown cause")
	}
	if !backgroundFinished.Load() {
		t.Fatal("serve() returned before the background group drained")
	}
	if backgroundCancelled.Load() {
		t.Fatal("serve() cancelled a background task that would have finished on its own")
	}
}
