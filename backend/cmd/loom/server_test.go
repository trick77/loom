package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/background"
	"github.com/trick77/ship/kit/httpx"
)

func TestServeReturnsListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close() // Serve on a closed listener fails at once.

	srv := httpx.NewServer(ln.Addr().String(), http.NewServeMux())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	cancelBase := testBase(t, srv)
	go func() { done <- serve(ctx, srv, ln, background.New(ctx), cancelBase, func(context.Context) {}) }()

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
	srv := httpx.NewServer(ln.Addr().String(), mux)

	ctx, cancel := context.WithCancel(context.Background())
	bg := background.New(context.Background())
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
	cancelBase := testBase(t, srv)
	go func() { served <- serve(ctx, srv, ln, bg, cancelBase, func(context.Context) {}) }()
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

// The database closes right after serve() returns, so a worker still writing
// must have stopped by then, whether a signal or a listener failure ends it.
func TestServeWaitsForWorkersToStop(t *testing.T) {
	for _, listenerFails := range []bool{false, true} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		srv := httpx.NewServer(ln.Addr().String(), http.NewServeMux())
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		var stopped atomic.Bool
		worker := func(ctx context.Context) {
			close(started)
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond) // finishing its last write
			stopped.Store(true)
		}

		served := make(chan error, 1)
		cancelBase := testBase(t, srv)
		go func() { served <- serve(ctx, srv, ln, background.New(context.Background()), cancelBase, worker) }()
		<-started
		if listenerFails {
			_ = ln.Close()
		} else {
			cancel()
		}

		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Fatalf("serve() did not return (listener fails: %v)", listenerFails)
		}
		if !stopped.Load() {
			t.Fatalf("serve() returned while a worker was still running (listener fails: %v)", listenerFails)
		}
		cancel()
	}
}

// An ordinary request in flight when the signal arrives finishes with its
// context intact: only what outlives the grace is cancelled.
func TestServeShutdownLetsShortRequestsFinish(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	requestStarted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /short", func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(200 * time.Millisecond)
		if r.Context().Err() != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httpx.NewServer(ln.Addr().String(), mux)
	ctx, cancel := context.WithCancel(context.Background())
	bg := background.New(context.Background())

	served := make(chan error, 1)
	cancelBase := testBase(t, srv)
	go func() { served <- serve(ctx, srv, ln, bg, cancelBase, func(context.Context) {}) }()
	status := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/short")
		if err != nil {
			status <- 0
			return
		}
		_ = resp.Body.Close()
		status <- resp.StatusCode
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("request never reached the handler")
	}

	cancel()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after the signal")
	}
	if got := <-status; got != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want 200 (it was cancelled instead of allowed to finish)", got)
	}
}

// testBase gives srv a fresh base context, as runServer() does, and returns its
// cancel for serve.
func testBase(t *testing.T, srv *http.Server) context.CancelCauseFunc {
	t.Helper()
	base, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	srv.BaseContext = func(net.Listener) context.Context { return base }
	return cancel
}
