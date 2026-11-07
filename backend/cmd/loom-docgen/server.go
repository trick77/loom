package main

import (
	"net/http"

	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/toolkit/mcpserver"
)

// generators are the document tools, PDFs rendered by gotenberg.
func generators(gotenberg *docgen.GotenbergClient) []docgen.Generator {
	return []docgen.Generator{
		docgen.TextGenerator{},
		docgen.NewPDFGenerator(gotenberg),
		docgen.XLSXGenerator{},
		docgen.DOCXGenerator{},
		docgen.PPTXGenerator{},
	}
}

// newServer mounts kit's health probes (loopback only, the container's own
// healthcheck) and, behind the bearer token, the document tools at /mcp.
func newServer(cfg config, gens []docgen.Generator, version string) http.Handler {
	api := http.NewServeMux()
	api.Handle("/mcp", mcpserver.New(docgen.MCPServer(gens, version)))
	return sidecar.Handler(cfg.token, api)
}
