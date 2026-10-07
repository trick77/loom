// Package sandbox is loom's client for the loom-sandbox sidecar, which runs the
// model's run_python code. The sidecar is optional: when it is
// unconfigured or unhealthy the tool is simply not offered, and chat works as
// before.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Errors a caller tells the model about in plain words.
var (
	ErrBusy        = errors.New("sandbox busy")
	ErrUnavailable = errors.New("sandbox unavailable")
	// ErrRejected wraps the sidecar's reason for refusing a malformed job.
	ErrRejected = errors.New("sandbox rejected the job")
)

const (
	maxResponseBytes = 40 << 20
	// requestOverhead is how much longer loom waits than the job may run: the
	// queue wait for a slot, process start and output collection.
	requestOverhead = 20 * time.Second
	probeTimeout    = 5 * time.Second
)

// File is one input or output file. Names are the sidecar-safe aliases.
type File struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Request is one stateless job.
type Request struct {
	Code    string
	Files   []File
	Timeout time.Duration
}

// Result is what the job printed and wrote. A non-zero ExitCode is a normal
// result (the model reads the traceback and fixes its code), not an error.
type Result struct {
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
	ExitCode   int      `json:"exit_code"`
	TimedOut   bool     `json:"timed_out"`
	Truncated  bool     `json:"truncated"`
	Files      []File   `json:"files"`
	Dropped    []string `json:"dropped"`
	DurationMS int64    `json:"duration_ms"`
}

// Client talks to one sidecar.
type Client struct {
	baseURL   string
	token     string
	timeout   time.Duration
	http      *http.Client
	available atomic.Bool
	// tokenRejected remembers that the last probe failed on the token, so the
	// error is logged once rather than every probe.
	tokenRejected atomic.Bool
	// wake tells Watch that a run withdrew the tool, so it probes on the
	// down interval instead of finishing a long up-interval wait.
	wake chan struct{}
	down time.Duration // downProbeInterval; tests shorten it
}

// New returns a client for the sidecar at baseURL. timeout is the longest a
// job may run.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		timeout: timeout,
		http:    &http.Client{},
		wake:    make(chan struct{}, 1),
		down:    downProbeInterval,
	}
}

// Timeout is the longest a job may run.
func (c *Client) Timeout() time.Duration { return c.timeout }

// Available reports whether the last health probe passed.
func (c *Client) Available() bool { return c.available.Load() }

// Ping checks the sidecar's health endpoint once.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	// The sidecar checks the token on /healthz too, so a mismatch fails the
	// probe and keeps the tool withdrawn.
	if c.token != "" {
		req.Header.Set("X-Sandbox-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errTokenMismatch
	default:
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
}

var errTokenMismatch = errors.New("token rejected: BACKEND_SANDBOX_TOKEN must match the sidecar's SANDBOX_TOKEN")

// Probe runs one health check and records the outcome, logging only changes.
func (c *Client) Probe(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	err := c.Ping(pctx)
	if ctx.Err() != nil {
		// Shutting down: an interrupted probe says nothing about the sidecar.
		return
	}
	was := c.available.Swap(err == nil)
	// A wrong token is a configuration error, not an outage: say so loudly,
	// once, whether or not the tool was offered before.
	mismatch := errors.Is(err, errTokenMismatch)
	if mismatch && !c.tokenRejected.Swap(true) {
		slog.Error("sandbox rejects loom's token; run_python stays off", "url", c.baseURL, "err", err)
		return
	}
	if !mismatch {
		c.tokenRejected.Store(false)
	}
	switch {
	case err == nil && !was:
		slog.Info("sandbox available, run_python offered", "url", c.baseURL)
	case err != nil && was:
		slog.Warn("sandbox unreachable, run_python withdrawn", "url", c.baseURL, "err", err)
	case err != nil:
		slog.Debug("sandbox still unreachable", "url", c.baseURL, "err", err)
	}
}

// downProbeInterval is how often Watch probes while the sidecar is down, so
// the tool comes back soon after it does (at boot, loom may start first).
const downProbeInterval = 10 * time.Second

// Watch probes now and then until ctx ends: every interval while the sidecar
// is up, every downProbeInterval while it is down. The tool appears once the
// sidecar answers and disappears while it does not.
func (c *Client) Watch(ctx context.Context, interval time.Duration) {
	c.Probe(ctx)
	if !c.Available() {
		slog.Info("sandbox not reachable yet; run_python is offered once it is", "url", c.baseURL)
	}
	for {
		next := interval
		if !c.Available() && c.down < next {
			next = c.down
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
			// A run just withdrew the tool: start the faster down rhythm now.
		case <-time.After(next):
			c.Probe(ctx)
		}
	}
}

// Run executes one job.
func (c *Client) Run(ctx context.Context, r Request) (Result, error) {
	timeout := r.Timeout
	if timeout <= 0 || timeout > c.timeout {
		timeout = c.timeout
	}
	body, err := json.Marshal(struct {
		Code      string `json:"code"`
		Files     []File `json:"files,omitempty"`
		TimeoutMS int64  `json:"timeout_ms"`
	}{r.Code, r.Files, timeout.Milliseconds()})
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+requestOverhead)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Sandbox-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil && errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("%w: no response in time", ErrUnavailable)
		}
		if ctx.Err() == nil && c.withdraw() {
			// The sidecar is gone: withdraw the tool now rather than at the next
			// probe; Watch brings it back once it answers again.
			slog.Warn("sandbox unreachable, run_python withdrawn", "url", c.baseURL, "err", err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(data) > maxResponseBytes {
		return Result{}, errors.New("sandbox response too large")
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return Result{}, ErrBusy
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return Result{}, fmt.Errorf("%w: %s", ErrRejected, errorMessage(data))
	case http.StatusUnauthorized:
		if c.withdraw() {
			slog.Error("sandbox rejected loom's token, run_python withdrawn", "url", c.baseURL)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, errTokenMismatch)
	default:
		return Result{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	var out Result
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("decode sandbox result: %w", err)
	}
	return out, nil
}

func errorMessage(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return "invalid request"
}

// withdraw takes the tool away after a failed run and wakes Watch, so it
// brings the tool back on the short down interval. It reports whether the
// tool was offered until now.
func (c *Client) withdraw() bool {
	was := c.available.Swap(false)
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return was
}
