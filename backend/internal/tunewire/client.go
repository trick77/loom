// Package tunewire is loom's client for the loom-tunewire sidecar's token
// routes: the settings page reads the signed-in user's Apple Music connection
// and hands that user's media-user-token through. Every loom user has their
// own; loom never stores one, the sidecar keeps them per user.
package tunewire

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/trick77/loom/internal/sidecar"
)

// requestTimeout covers a token check: the sidecar may first fetch the web
// player's token (up to 45s), then asks Apple twice. Should loom give up
// anyway, the sidecar stores nothing.
const requestTimeout = 2 * time.Minute

// ErrUnavailable means the sidecar could not be reached or failed.
var ErrUnavailable = sidecar.ErrUnavailable

// Codes say why a call failed, so the settings page can word it without the
// sidecar's own (English, internal) text. A refused token leaves the old one in
// place.
const (
	CodeMalformed = "malformed"         // the pasted text is no token at all
	CodeRejected  = "rejected"          // Apple refused the token
	CodeBridge    = "bridge"            // Apple changed what the bridge relies on
	CodeUpstream  = "upstream"          // Apple failed or could not be reached
	CodeStorage   = sidecar.CodeStorage // the sidecar could not write /data
	CodeConfig    = sidecar.CodeConfig  // loom's and the sidecar's tokens differ
)

// Error is a call the sidecar answered with a reason.
type Error = sidecar.Error

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
type Client struct{ c *sidecar.Client }

// New returns a client for the sidecar at baseURL, authenticating with token.
func New(baseURL, token string) *Client {
	return &Client{c: sidecar.New("tunewire", baseURL, token, "BACKEND_TUNEWIRE_TOKEN", sidecar.Codes{
		Unprocessable: []string{CodeRejected, CodeMalformed},
		BadGateway:    []string{CodeUpstream, CodeBridge},
	}, requestTimeout)}
}

// Status reads user's own connection; check asks Apple first.
func (c *Client) Status(ctx context.Context, user string, check bool) (Status, error) {
	path := "/status"
	if check {
		path += "?check=1"
	}
	var st Status
	return st, c.c.Do(ctx, user, http.MethodGet, path, nil, &st)
}

// SetToken hands user's pasted token to the sidecar, which stores it as that
// user's only if Apple accepts it.
func (c *Client) SetToken(ctx context.Context, user, token string) (Status, error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return Status{}, err
	}
	var st Status
	return st, c.c.Do(ctx, user, http.MethodPut, "/token", body, &st)
}

// ClearToken makes the sidecar forget user's token; other users' stay.
func (c *Client) ClearToken(ctx context.Context, user string) (Status, error) {
	var st Status
	return st, c.c.Do(ctx, user, http.MethodDelete, "/token", nil, &st)
}
