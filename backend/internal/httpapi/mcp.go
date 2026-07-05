package httpapi

import (
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/mcp"
)

// handleMCPServers backs the /mcp slash command: a live status snapshot of every
// configured MCP server (reachability, transport, endpoint, origin, tool count,
// and the failure reason when down). Endpoints are credential-free by
// construction (see mcp.ServerStatus).
func (s *server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	servers := []mcp.ServerStatus{}
	if s.mcp != nil {
		if live := s.mcp.ServerStatus(r.Context()); live != nil {
			servers = live
		}
	}
	writeJSON(w, map[string]any{"servers": servers})
}

// mcpToolInfo is one row of the /tools slash command.
type mcpToolInfo struct {
	Name        string   `json:"name"`
	Server      string   `json:"server"`
	Description string   `json:"description"`
	Required    []string `json:"required"`
}

// handleMCPTools backs the /tools slash command: every MCP tool the configured
// servers advertise, with its server, description, and required arguments. This
// is the full configured surface — the catalog of what is available in principle.
// Per-turn category gating (see availableTools) may inject only a subset of these
// into any given request, so a tool listed here is not guaranteed to be offered
// on every turn.
func (s *server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tools := []mcpToolInfo{}
	if s.mcp != nil {
		for _, t := range s.mcp.Tools() {
			server, _, _ := mcp.SplitExposedToolName(t.Function.Name)
			tools = append(tools, mcpToolInfo{
				Name:        t.Function.Name,
				Server:      server,
				Description: t.Function.Description,
				Required:    requiredParams(t.Function.Parameters),
			})
		}
	}
	writeJSON(w, map[string]any{"tools": tools})
}

// requiredParams pulls the JSON-schema "required" string array out of a tool's
// parameters, tolerating a missing or malformed field.
func requiredParams(params map[string]any) []string {
	raw, ok := params["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if name, ok := v.(string); ok {
			out = append(out, name)
		}
	}
	return out
}
