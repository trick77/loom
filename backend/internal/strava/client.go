// Package strava is loom's client for the loom-strava sidecar's connection
// routes: the settings page reads the signed-in user's Strava connection,
// starts Strava's OAuth and hands the code back. Every loom user has their own
// connection; loom never sees a Strava token, the sidecar keeps them per user.
package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/trick77/loom/internal/sidecar"
)

// requestTimeout covers a code exchange or a revoke: one Strava call each.
const requestTimeout = 30 * time.Second

// ErrUnavailable means the sidecar could not be reached or failed.
var ErrUnavailable = sidecar.ErrUnavailable

// Codes say why a call failed, so the settings page can word it without the
// sidecar's own text.
const (
	CodeRejected  = "rejected"          // Strava refused the code (expired, used, wrong app)
	CodeMalformed = "malformed"         // loom handed on something that is no code
	CodeUpstream  = "upstream"          // Strava failed or could not be reached
	CodeStorage   = sidecar.CodeStorage // the sidecar could not write /data
	CodeConfig    = sidecar.CodeConfig  // loom's and the sidecar's tokens differ
)

// Error is a call the sidecar answered with a reason.
type Error = sidecar.Error

// Athlete is the connected Strava account, as the settings page names it.
type Athlete struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Status is a user's connection as the settings page shows it. It never
// carries a token.
type Status struct {
	State         string    `json:"state"` // never, connected, expired, error
	Athlete       *Athlete  `json:"athlete,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	MissingScopes []string  `json:"missingScopes,omitempty"`
	ConnectedAt   time.Time `json:"connectedAt,omitzero"`
	LastOKAt      time.Time `json:"lastOkAt,omitzero"`
	LastError     string    `json:"lastError,omitempty"`
}

// Client talks to one sidecar.
type Client struct{ c *sidecar.Client }

// New returns a client for the sidecar at baseURL, authenticating with token.
func New(baseURL, token string) *Client {
	return &Client{c: sidecar.New("strava", baseURL, token, "BACKEND_STRAVA_TOKEN", sidecar.Codes{
		Unprocessable: []string{CodeRejected, CodeMalformed},
		BadGateway:    []string{CodeUpstream},
	}, requestTimeout)}
}

// Status reads user's own connection; check asks Strava first.
func (c *Client) Status(ctx context.Context, user string, check bool) (Status, error) {
	path := "/status"
	if check {
		path += "?check=1"
	}
	var st Status
	return st, c.c.Do(ctx, user, http.MethodGet, path, nil, &st)
}

// AuthorizeURL is Strava's consent page for user, coming back to redirectURI
// with state. The sidecar builds it: the client id stays there.
func (c *Client) AuthorizeURL(ctx context.Context, user, redirectURI, state string) (string, error) {
	q := url.Values{"redirect_uri": {redirectURI}, "state": {state}}
	var out struct {
		URL string `json:"url"`
	}
	if err := c.c.Do(ctx, user, http.MethodGet, "/authorize?"+q.Encode(), nil, &out); err != nil {
		return "", err
	}
	if u, err := url.Parse(out.URL); err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return "", fmt.Errorf("%w: strava: bad authorize URL from the sidecar", ErrUnavailable)
	}
	return out.URL, nil
}

// Connect hands Strava's code for user to the sidecar, which exchanges it and
// stores the connection as that user's.
func (c *Client) Connect(ctx context.Context, user, code, scope string) (Status, error) {
	body, err := json.Marshal(map[string]string{"code": code, "scope": scope})
	if err != nil {
		return Status{}, err
	}
	var st Status
	return st, c.c.Do(ctx, user, http.MethodPut, "/connection", body, &st)
}

// Disconnect revokes user's connection at Strava and deletes it with that
// user's data; other users' stay.
func (c *Client) Disconnect(ctx context.Context, user string) (Status, error) {
	var st Status
	return st, c.c.Do(ctx, user, http.MethodDelete, "/connection", nil, &st)
}
