// Package sandbox is loom's client for the loom-sandbox sidecar, which runs the
// model's run_python code under gVisor. The sidecar is optional: when it is
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
}

// New returns a client for the sidecar at baseURL. timeout is the longest a
// job may run.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		timeout: timeout,
		http:    &http.Client{},
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
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

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
	switch {
	case err == nil && !was:
		slog.Info("sandbox available, run_python offered", "url", c.baseURL)
	case err != nil && was:
		slog.Warn("sandbox unreachable, run_python withdrawn", "url", c.baseURL, "err", err)
	case err != nil:
		slog.Debug("sandbox still unreachable", "url", c.baseURL, "err", err)
	}
}

// Watch probes now and then every interval until ctx ends, so the tool
// appears once the sidecar comes up and disappears while it is down.
func (c *Client) Watch(ctx context.Context, interval time.Duration) {
	c.Probe(ctx)
	if !c.Available() {
		slog.Warn("sandbox not reachable at boot; run_python stays off until it is", "url", c.baseURL)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
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
	req.Header.Set("X-Sandbox-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil && errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("%w: no response in time", ErrUnavailable)
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
		return Result{}, fmt.Errorf("sandbox rejected the job: %s", errorMessage(data))
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
