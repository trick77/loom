package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "0123456789abcdef"

type fakeExecutor struct {
	mu      sync.Mutex
	jobs    []job
	block   chan struct{}
	started chan struct{}
	err     error
}

func (f *fakeExecutor) run(ctx context.Context, j job) (runResponse, error) {
	f.mu.Lock()
	f.jobs = append(f.jobs, j)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return runResponse{}, ctx.Err()
		}
	}
	if f.err != nil {
		return runResponse{}, f.err
	}
	return runResponse{Stdout: "42\n", Files: []wireFile{{Name: "a.png", Data: []byte{1, 2}}}}, nil
}

func testServer(exec executor, slots int) *server {
	return newServer(config{token: testToken, slots: slots, queueWait: 50 * time.Millisecond}, exec)
}

func post(t *testing.T, h http.Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case string:
		buf.WriteString(b)
	default:
		if err := json.NewEncoder(&buf).Encode(b); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/run", &buf)
	if token != "" {
		req.Header.Set("X-Sandbox-Token", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRunRequiresToken(t *testing.T) {
	f := &fakeExecutor{}
	s := testServer(f, 1)
	for _, tok := range []string{"", "wrong-token-value"} {
		if rec := post(t, s, tok, runRequest{Code: "print(1)"}); rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d", tok, rec.Code)
		}
	}
	if len(f.jobs) != 0 {
		t.Fatal("job ran without a valid token")
	}
}

func TestRunHappyPath(t *testing.T) {
	f := &fakeExecutor{}
	rec := post(t, testServer(f, 1), testToken, runRequest{Code: "print(42)", TimeoutMS: 5000})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp runResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "42\n" || len(resp.Files) != 1 || !bytes.Equal(resp.Files[0].Data, []byte{1, 2}) {
		t.Fatalf("resp %+v", resp)
	}
	if f.jobs[0].timeout != 5*time.Second {
		t.Fatalf("timeout %v", f.jobs[0].timeout)
	}
}

func TestRunRejectsBadBodies(t *testing.T) {
	s := testServer(&fakeExecutor{}, 1)
	if rec := post(t, s, testToken, "{not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", rec.Code)
	}
	if rec := post(t, s, testToken, runRequest{Code: ""}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty code: %d", rec.Code)
	}
	huge := `{"code":"` + strings.Repeat("x", maxRequestBody) + `"}`
	if rec := post(t, s, testToken, huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge body: %d", rec.Code)
	}
}

func TestRunBusyWhenSlotsTaken(t *testing.T) {
	f := &fakeExecutor{block: make(chan struct{}), started: make(chan struct{}, 1)}
	s := testServer(f, 1)
	first := make(chan int, 1)
	go func() { first <- post(t, s, testToken, runRequest{Code: "1"}).Code }()
	<-f.started

	if rec := post(t, s, testToken, runRequest{Code: "2"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second job: status %d", rec.Code)
	}
	close(f.block)
	if code := <-first; code != http.StatusOK {
		t.Fatalf("first job: status %d", code)
	}
	// The slot came back.
	f.block = nil
	f.started = nil
	if rec := post(t, s, testToken, runRequest{Code: "3"}); rec.Code != http.StatusOK {
		t.Fatalf("after release: status %d", rec.Code)
	}
}

func TestRunWithoutTokenAcceptsAnyClient(t *testing.T) {
	s := newServer(config{slots: 1, queueWait: 50 * time.Millisecond}, &fakeExecutor{})
	if rec := post(t, s, "", runRequest{Code: "1"}); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

// A request waiting for a slot must not have its body read yet: queued
// requests would otherwise hold their full inputs in memory.
func TestRunTakesTheSlotBeforeReadingTheBody(t *testing.T) {
	f := &fakeExecutor{block: make(chan struct{}), started: make(chan struct{}, 1)}
	s := testServer(f, 1)
	first := make(chan int, 1)
	go func() { first <- post(t, s, testToken, runRequest{Code: "1"}).Code }()
	<-f.started
	body := &countingReader{r: strings.NewReader(`{"code":"2"}`)}
	req := httptest.NewRequest(http.MethodPost, "/run", body)
	req.Header.Set("X-Sandbox-Token", testToken)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	close(f.block)
	<-first
	if rec.Code != http.StatusTooManyRequests || body.n != 0 {
		t.Fatalf("status %d, %d body bytes read while waiting", rec.Code, body.n)
	}
}

type countingReader struct {
	r *strings.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestRunExecutorErrorIs500(t *testing.T) {
	rec := post(t, testServer(&fakeExecutor{err: errors.New("boom")}, 1), testToken, runRequest{Code: "1"})
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(&fakeExecutor{}, 1).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHealthcheckCommand(t *testing.T) {
	ts := httptest.NewServer(testServer(&fakeExecutor{}, 1))
	defer ts.Close()
	if err := healthcheck(strings.TrimPrefix(ts.URL, "http://")); err != nil {
		t.Fatal(err)
	}
	if err := healthcheck("127.0.0.1:1"); err == nil {
		t.Fatal("want an error for a closed port")
	}
}
