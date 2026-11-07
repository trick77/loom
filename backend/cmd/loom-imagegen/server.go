package main

import (
	"net/http"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, the image tool at /mcp.
func newServer(cfg config, svc *imagegen.Service, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(imagegen.MCPServer(svc, version)))
	return sidecar.Handler(cfg.token, api)
}
