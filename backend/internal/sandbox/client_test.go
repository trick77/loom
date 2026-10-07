package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSendsJobAndDecodesResult(t *testing.T) {
	var got struct {
		Code      string `json:"code"`
		Files     []File `json:"files"`
		TimeoutMS int64  `json:"timeout_ms"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run" || r.Header.Get("X-Sandbox-Token") != "tok" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(Result{Stdout: "3\n", ExitCode: 0, Files: []File{{Name: "a.png", Data: []byte{1}}}})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "tok", 30*time.Second)
	res, err := c.Run(context.Background(), Request{Code: "print(1+2)", Files: []File{{Name: "in.csv", Data: []byte("a")}}, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "3\n" || len(res.Files) != 1 || res.Files[0].Data[0] != 1 {
		t.Fatalf("result %+v", res)
	}
	if got.Code != "print(1+2)" || got.TimeoutMS != 30000 || got.Files[0].Name != "in.csv" {
		t.Fatalf("request %+v (timeout must be clamped to the client's)", got)
	}
}

func TestRunMapsStatuses(t *testing.T) {
	cases := map[int]func(error) bool{
		http.StatusTooManyRequests:       func(err error) bool { return errors.Is(err, ErrBusy) },
		http.StatusInternalServerError:   func(err error) bool { return errors.Is(err, ErrUnavailable) },
		http.StatusBadRequest:            func(err error) bool { return err != nil && strings.Contains(err.Error(), "code is empty") },
		http.StatusRequestEntityTooLarge: func(err error) bool { return err != nil && strings.Contains(err.Error(), "rejected") },
	}
	for status, ok := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"code is empty"}`))
		}))
		_, err := New(srv.URL, "tok", time.Second).Run(context.Background(), Request{Code: "x"})
		srv.Close()
		if !ok(err) {
			t.Errorf("status %d: err %v", status, err)
		}
	}
}

func TestRunUnreachableIsUnavailable(t *testing.T) {
	c := New("http://127.0.0.1:1", "tok", time.Second)
	c.available.Store(true)
	_, err := c.Run(context.Background(), Request{Code: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if c.Available() {
		t.Fatal("a refused connection must withdraw the tool at once")
	}
}

func TestRunWithoutTokenSendsNoHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["X-Sandbox-Token"]; ok {
			http.Error(w, "unexpected token", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(Result{Stdout: "ok"})
	}))
	defer srv.Close()
	res, err := New(srv.URL, "", time.Second).Run(context.Background(), Request{Code: "x"})
	if err != nil || res.Stdout != "ok" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRunBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok", time.Second).Run(context.Background(), Request{Code: "x"}); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestProbeTracksAvailability(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", time.Second)
	if c.Available() {
		t.Fatal("available before any probe")
	}
	c.Probe(context.Background())
	if !c.Available() {
		t.Fatal("healthy sidecar not available")
	}
	healthy.Store(false)
	c.Probe(context.Background())
	if c.Available() {
		t.Fatal("unhealthy sidecar still available")
	}
	if c.Timeout() != time.Second {
		t.Fatal("timeout")
	}
}

func TestWatchStopsWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Watch(ctx, 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Watch did not return")
	}
	if !c.Available() {
		t.Fatal("not available after probes")
	}
}
