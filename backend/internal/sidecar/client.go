// Package sidecar is what loom and its sidecars share: the token rule
// (TokenProblem), the sidecar's bearer-token root (Handler), and loom's
// client plumbing for the per-user sidecars' connection routes (tunewire,
// strava): every request names the signed-in loom user (loomuser.Header)
// and carries loom's bearer token; failures come back as *Error with the
// sidecar's code, so the settings page can word them without the sidecar's
// own text. Every sidecar image copies this package, so it imports nothing
// of loom but loomuser (TestImportsNoOtherLoomPackage).
package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/loom/internal/loomuser"
)

// ErrUnavailable means a sidecar could not be reached or failed.
var ErrUnavailable = errors.New("sidecar unavailable")

// The codes every sidecar shares: the sidecar's storage failed (500), or
// loom's and the sidecar's tokens differ (401).
const (
	CodeStorage = "storage"
	CodeConfig  = "config"
)

// maxResponseBytes bounds a connection route's answer: one status.
const maxResponseBytes = 64 << 10

// Error is a call the sidecar answered with a reason. It wraps ErrUnavailable
// for the codes that mean the sidecar itself cannot serve (storage, config).
type Error struct {
	Name   string // the sidecar, for the error text
	Code   string
	Reason string
}

func (e *Error) Error() string { return e.Name + ": " + e.Code + ": " + e.Reason }

func (e *Error) Unwrap() error {
	if e.Code == CodeStorage || e.Code == CodeConfig {
		return ErrUnavailable
	}
	return nil
}

// Codes says which codes a sidecar may send with a 422 and a 502: the first
// of each list is the default when the body names none of them.
type Codes struct {
	Unprocessable []string
	BadGateway    []string
}

// Client talks to one sidecar's connection routes.
type Client struct {
	name     string
	baseURL  string
	token    string
	tokenEnv string // loom's setting for the token, named in a config error
	codes    Codes
	http     *http.Client
}

// New returns a client for the sidecar name at baseURL, authenticating with
// token (loom's tokenEnv), mapping failures by codes.
func New(name, baseURL, token, tokenEnv string, codes Codes, timeout time.Duration) *Client {
	return &Client{name: name, baseURL: strings.TrimRight(baseURL, "/"), token: token, tokenEnv: tokenEnv, codes: codes,
		http: &http.Client{Timeout: timeout}}
}

// Do sends a request for user and decodes a 200 answer into out.
func (c *Client) Do(ctx context.Context, user, method, path string, body []byte, out any) error {
	if user == "" {
		// Every account is one person's: a call for nobody is a bug here.
		return fmt.Errorf("%w: %s: no loom user", ErrUnavailable, c.name)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(loomuser.Header, user)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, c.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, c.name, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: %s: bad JSON: %w", ErrUnavailable, c.name, err)
		}
		return nil
	case http.StatusUnprocessableEntity:
		return &Error{Name: c.name, Code: codeOf(raw, c.codes.Unprocessable), Reason: errorText(raw)}
	case http.StatusBadGateway:
		return &Error{Name: c.name, Code: codeOf(raw, c.codes.BadGateway), Reason: errorText(raw)}
	case http.StatusInternalServerError:
		return &Error{Name: c.name, Code: CodeStorage, Reason: errorText(raw)}
	case http.StatusUnauthorized:
		// The two tokens differ: a deploy problem, not the user's.
		return &Error{Name: c.name, Code: CodeConfig, Reason: "the sidecar refused loom's " + c.tokenEnv}
	default:
		return fmt.Errorf("%w: %s: %s returned %d", ErrUnavailable, c.name, strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
}

// codeOf is the body's code when it is one of allowed, else allowed[0].
func codeOf(raw []byte, allowed []string) string {
	if len(allowed) == 0 {
		return ""
	}
	var e struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &e) == nil {
		for _, a := range allowed {
			if e.Code == a {
				return a
			}
		}
	}
	return allowed[0]
}

func errorText(raw []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		return e.Error
	}
	return "no reason given"
}
