package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/loom/internal/config"
)

func TestSandboxForConfigUnsetIsNil(t *testing.T) {
	runner, watch := sandboxForConfig(config.Config{})
	if runner != nil {
		t.Fatalf("runner = %#v, want a nil interface", runner)
	}
	done := make(chan struct{})
	go func() { watch(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch must return at once without a sandbox")
	}
}

func TestSandboxForConfigWatchesTheSidecar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	runner, watch := sandboxForConfig(config.Config{SandboxURL: srv.URL, SandboxToken: "0123456789abcdef", SandboxTimeout: time.Minute})
	if runner == nil || runner.Available() || runner.Timeout() != time.Minute {
		t.Fatal("want an unprobed client")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watch(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for !runner.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if !runner.Available() {
		t.Fatal("healthy sidecar never became available")
	}
}
