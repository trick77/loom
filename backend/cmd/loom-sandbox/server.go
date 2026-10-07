package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type server struct {
	cfg   config
	exec  executor
	slots chan int
	mux   *http.ServeMux
}

func newServer(cfg config, exec executor) *server {
	s := &server{cfg: cfg, exec: exec, slots: make(chan int, cfg.slots), mux: http.NewServeMux()}
	for i := 0; i < cfg.slots; i++ {
		s.slots <- i
	}
	// /healthz checks the token too: loom's probe then fails on a mismatch and
	// withdraws the tool, instead of offering it while every run gets 401.
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("POST /run", s.handleRun)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) authorized(r *http.Request) bool {
	if s.cfg.token == "" {
		return true
	}
	got := r.Header.Get("X-Sandbox-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.token)) == 1
}

func (s *server) handleRun(w http.ResponseWriter, r *http.Request) {
	defer coverageFlush()
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// The slot comes first: a request waiting for one holds only its headers,
	// so queued requests cannot add up to more memory than the slots allow.
	slot, ok := s.acquire(r.Context())
	if !ok {
		writeError(w, http.StatusTooManyRequests, "sandbox busy")
		return
	}
	defer func() { s.slots <- slot }()

	var req runRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	j, err := validateRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	j.slot = slot

	// The request context ends when loom gives up (the user pressed stop); the
	// executor kills the job then, and on its own timeout.
	start := time.Now()
	resp, err := s.exec.run(r.Context(), j)
	if err != nil {
		if r.Context().Err() != nil {
			// loom gave up (the user pressed stop): the job is killed and nobody
			// is left to read a response. Not a sandbox fault.
			slog.Info("sandbox job cancelled by the client")
			return
		}
		slog.Error("sandbox job failed", "err", err)
		writeError(w, http.StatusInternalServerError, "sandbox error")
		return
	}
	resp.DurationMS = time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) acquire(ctx context.Context) (int, bool) {
	t := time.NewTimer(s.cfg.queueWait)
	defer t.Stop()
	select {
	case slot := <-s.slots:
		return slot, true
	case <-t.C:
		return 0, false
	case <-ctx.Done():
		return 0, false
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
