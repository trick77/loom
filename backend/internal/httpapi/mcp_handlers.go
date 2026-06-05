package httpapi

import (
	"context"
	"net/http"
)

func (s *server) handleMCPStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.currentMCPStatus(r.Context()))
}

func (s *server) currentMCPStatus(ctx context.Context) mcpStatusResponse {
	if s.mcp == nil {
		return mcpStatusResponse{}
	}
	statuses := s.mcp.ServerStatus(ctx)
	active := 0
	servers := make([]mcpServerStatus, 0, len(statuses))
	for _, st := range statuses {
		if st.Active {
			active++
		}
		servers = append(servers, mcpServerStatus{Name: st.Name, Active: st.Active})
	}
	return mcpStatusResponse{Active: active, Configured: len(statuses), Servers: servers}
}
