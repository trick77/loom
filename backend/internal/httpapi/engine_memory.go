package httpapi

import (
	"context"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
)

// engineMemory hands the server's memory code to the turn engine as its
// Memory port. The memory stays on the server: the background worker and the
// request path share its single-flight guard.
type engineMemory struct {
	s *server
}

func (m engineMemory) UserContext(ctx context.Context, userID string) string {
	return m.s.userContextForUser(ctx, userID)
}

func (m engineMemory) ProjectContext(ctx context.Context, userID string, thread chat.Thread) string {
	return m.s.projectContextForThread(ctx, userID, thread)
}

func (m engineMemory) RefreshProjectDescription(ctx context.Context, user auth.User, projectID string) {
	m.s.maybeRefreshProjectDescriptionAsync(ctx, user, projectID)
}
