// Package loomuser is how loom names the user a sidecar call is made for. It imports nothing of loom, so a
// sidecar image copies this directory alone.
package loomuser

import (
	"net/http"
	"strings"
)

// Header carries the loom user id on every call loom makes to a per-user sidecar. Only loom holds the bearer
// token the routes sit behind, so the header is as trustworthy as that token. A sidecar's MCP endpoint names it
// as mcpserver.Server.UserHeader; its own routes read it with FromRequest.
const Header = "X-Loom-User"

// FromRequest is the request's loom user id (Header), trimmed.
func FromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(Header))
}
