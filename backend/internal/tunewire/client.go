// Package tunewire is loom's client for the loom-tunewire sidecar's token
// routes: the settings page reads the signed-in user's Apple Music connection
// and hands that user's media-user-token through. Every loom user has their
// own; loom never stores one, the sidecar keeps them per user.
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

	"github.com/trick77/loom/internal/mcpserver"
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

// Codes say why a call failed, so the settings page can word it without the
// sidecar's own (English, internal) text. A refused token leaves the old one in
// place.
const (
	CodeMalformed = "malformed" // the pasted text is no token at all
	CodeRejected  = "rejected"  // Apple refused the token
	CodeBridge    = "bridge"    // Apple changed what the bridge relies on
	CodeUpstream  = "upstream"  // Apple failed or could not be reached
	CodeStorage   = "storage"   // the sidecar could not write /data
	CodeConfig    = "config"    // loom's and the sidecar's tokens differ
)

// Error is a call the sidecar answered with a reason. It wraps ErrUnavailable
// for the codes that mean the sidecar itself cannot serve (storage, config).
type Error struct{ Code, Reason string }

func (e *Error) Error() string { return "tunewire: " + e.Code + ": " + e.Reason }

func (e *Error) Unwrap() error {
	if e.Code == CodeStorage || e.Code == CodeConfig {
		return ErrUnavailable
	}
	return nil
}

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

// Status reads user's own connection; check asks Apple first.
func (c *Client) Status(ctx context.Context, user string, check bool) (Status, error) {
	path := "/status"
	if check {
		path += "?check=1"
	}
	return c.do(ctx, user, http.MethodGet, path, nil)
}

// SetToken hands user's pasted token to the sidecar, which stores it as that
// user's only if Apple accepts it.
func (c *Client) SetToken(ctx context.Context, user, token string) (Status, error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return Status{}, err
	}
	return c.do(ctx, user, http.MethodPut, "/token", body)
}

// ClearToken makes the sidecar forget user's token; other users' stay.
func (c *Client) ClearToken(ctx context.Context, user string) (Status, error) {
	return c.do(ctx, user, http.MethodDelete, "/token", nil)
}

func (c *Client) do(ctx context.Context, user, method, path string, body []byte) (Status, error) {
	if user == "" {
		// Every account is one person's: a call for nobody is a bug here.
		return Status{}, fmt.Errorf("%w: no loom user", ErrUnavailable)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(mcpserver.UserHeader, user)
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
		return Status{}, &Error{Code: codeOf(raw, CodeRejected, CodeMalformed), Reason: errorText(raw)}
	case http.StatusBadGateway:
		return Status{}, &Error{Code: codeOf(raw, CodeUpstream, CodeBridge), Reason: errorText(raw)}
	case http.StatusInternalServerError:
		return Status{}, &Error{Code: CodeStorage, Reason: errorText(raw)}
	case http.StatusUnauthorized:
		// The two tokens differ: a deploy problem, not the user's.
		return Status{}, &Error{Code: CodeConfig, Reason: "the sidecar refused loom's BACKEND_TUNEWIRE_TOKEN"}
	default:
		return Status{}, fmt.Errorf("%w: %s returned %d", ErrUnavailable, path, resp.StatusCode)
	}
}

// codeOf is the body's code when it is one of allowed, else def.
func codeOf(raw []byte, def string, allowed ...string) string {
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
	return def
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
