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
	for _, st := range statuses {
		if st.Active {
			active++
		}
	}
	return mcpStatusResponse{Active: active, Configured: len(statuses)}
}
