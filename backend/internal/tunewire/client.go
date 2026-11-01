// Package tunewire is loom's client for the loom-tunewire sidecar's token
// routes: the settings page reads the Apple Music connection's status and
// hands the user's media-user-token through. Loom never stores the token; the
// sidecar keeps it.
package tunewire

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
)

const (
	// requestTimeout covers a token check: the sidecar may first fetch the web
	// player's token (up to 45s), then asks Apple twice. Should loom give up
	// anyway, the sidecar stores nothing.
	requestTimeout   = 2 * time.Minute
	maxResponseBytes = 64 << 10
)

// ErrUnavailable means the sidecar could not be reached or failed.
var ErrUnavailable = errors.New("tunewire unavailable")

// RejectedError carries the sidecar's reason for refusing a token. Code says
// which: CodeMalformed (not a token at all) or CodeRejected (Apple refused
// it). The old token stays.
type RejectedError struct{ Reason, Code string }

// The sidecar's 422 codes.
const (
	CodeMalformed = "malformed"
	CodeRejected  = "rejected"
)

func (e *RejectedError) Error() string { return "tunewire rejected the token: " + e.Reason }

// UpstreamError means the sidecar could not reach Apple to check a token.
type UpstreamError struct{ Reason string }

func (e *UpstreamError) Error() string { return "tunewire could not reach Apple Music: " + e.Reason }

// Status is the connection as the settings page shows it. It never carries
// the token. PlaylistCount is absent while unknown; ErrorKind ("bridge" or
// "upstream") lets the page word an error without the sidecar's own text.
type Status struct {
	State         string    `json:"state"`
	Storefront    string    `json:"storefront,omitempty"`
	PlaylistCount *int      `json:"playlistCount,omitempty"`
	SetAt         time.Time `json:"setAt,omitzero"`
	LastOKAt      time.Time `json:"lastOkAt,omitzero"`
	LastError     string    `json:"lastError,omitempty"`
	ErrorKind     string    `json:"errorKind,omitempty"`
}

// Client talks to one sidecar.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for the sidecar at baseURL, authenticating with token.
func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: requestTimeout}}
}

// Status reads the connection; check asks Apple first.
func (c *Client) Status(ctx context.Context, check bool) (Status, error) {
	path := "/status"
	if check {
		path += "?check=1"
	}
	return c.do(ctx, http.MethodGet, path, nil)
}

// SetToken hands a pasted token to the sidecar, which stores it only if Apple
// accepts it.
func (c *Client) SetToken(ctx context.Context, token string) (Status, error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return Status{}, err
	}
	return c.do(ctx, http.MethodPut, "/token", body)
}

// ClearToken makes the sidecar forget the token.
func (c *Client) ClearToken(ctx context.Context) (Status, error) {
	return c.do(ctx, http.MethodDelete, "/token", nil)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (Status, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var st Status
		if err := json.Unmarshal(raw, &st); err != nil {
			return Status{}, fmt.Errorf("%w: bad status JSON: %w", ErrUnavailable, err)
		}
		return st, nil
	case http.StatusUnprocessableEntity:
		code := CodeRejected
		var e struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Code == CodeMalformed {
			code = CodeMalformed
		}
		return Status{}, &RejectedError{Reason: errorText(raw), Code: code}
	case http.StatusBadGateway:
		return Status{}, &UpstreamError{Reason: errorText(raw)}
	default:
		// 401 means the two tokens differ: a deploy problem, not the user's.
		return Status{}, fmt.Errorf("%w: %s returned %d", ErrUnavailable, path, resp.StatusCode)
	}
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
