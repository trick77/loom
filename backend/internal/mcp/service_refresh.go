package mcp

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"
)

const (
	// refreshListTimeout bounds one server's re-discovery.
	refreshListTimeout = 30 * time.Second
	// downServerRetryInterval is how soon a server that is not answering is
	// tried again: sooner than a healthy server's list goes stale, so one that
	// was down at boot joins within minutes instead of never.
	downServerRetryInterval = 2 * time.Minute
)

// RunRefresh re-discovers every remote server's tools until ctx ends, at the
// interval the servers' ttlMs asks for. A server that changed its tools (or
// their descriptions) is picked up without a restart, and a best-effort server
// that was down at boot is registered once it answers.
func (s *Service) RunRefresh(ctx context.Context, logger *slog.Logger) {
	if s == nil {
		return
	}
	delay := s.nextRefreshDelay()
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = s.Refresh(ctx, logger)
	}
}

// Refresh re-lists every remote server's tools once and swaps the results in.
// A server that fails keeps the tools it had (a required server never loses
// its tools to a blip); one that never answered stays absent. It returns how
// long until the next refresh is due.
func (s *Service) Refresh(ctx context.Context, logger *slog.Logger) time.Duration {
	s.mu.RLock()
	servers := make([]registeredServer, len(s.servers))
	for i, srv := range s.servers {
		servers[i] = *srv
	}
	s.mu.RUnlock()

	next := defaultToolListTTL
	updated := map[string]registeredServer{}
	for _, srv := range servers {
		cfg := s.cfg.Servers[srv.name]
		if cfg.Transport == TransportInProcess {
			continue
		}
		client := srv.client
		if client == nil {
			client = clientForServer(srv.name, cfg, s.httpClient)
		}
		listCtx, cancel := context.WithTimeout(ctx, refreshListTimeout)
		tools, err := client.ListTools(listCtx)
		cancel()
		if err != nil {
			if logger != nil {
				logger.Warn("MCP server refresh failed", "server", srv.name, "err", err)
			}
			if srv.client == nil {
				_ = client.Close()
			}
			next = min(next, downServerRetryInterval)
			continue
		}
		if meta, ok := client.(serverMetadata); ok {
			next = min(next, meta.ToolListTTL())
		}
		srv.client, srv.tools = client, tools
		updated[srv.name] = srv
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, srv := range s.servers {
		if fresh, ok := updated[srv.name]; ok {
			s.servers[i] = &fresh
		}
	}
	s.rebuildLocked(logger)
	return next
}

// nextRefreshDelay is the wait before the first refresh: the shortest ttl a
// server asked for, or the retry interval when a server is still missing.
func (s *Service) nextRefreshDelay() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := defaultToolListTTL
	for _, srv := range s.servers {
		if srv.client == nil {
			next = min(next, downServerRetryInterval)
			continue
		}
		if meta, ok := srv.client.(serverMetadata); ok {
			next = min(next, meta.ToolListTTL())
		}
	}
	return next
}

// rebuildLocked re-derives the routes and the model-facing tool list from
// every server's last tool list, in registration order so a contested name
// stays with the server that held it first. The caller holds mu.
func (s *Service) rebuildLocked(logger *slog.Logger) {
	s.routes = map[string]toolRoute{}
	s.tools = nil
	for _, srv := range s.servers {
		if srv.client == nil {
			continue
		}
		_ = s.register(srv.name, srv.client, srv.tools, skipDuplicate(logger))
	}
}

// ServerInstructions returns a server's guidance for the model (its
// server/discover or initialize instructions), capped so one server cannot
// crowd the system prompt. Empty when the server gave none, or when its
// config does not trust them (ServerConfig.TrustInstructions).
func (s *Service) ServerInstructions(server string) string {
	if s == nil || !s.cfg.Servers[server].TrustInstructions {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, srv := range s.servers {
		if srv.name != server || srv.client == nil {
			continue
		}
		meta, ok := srv.client.(serverMetadata)
		if !ok {
			return ""
		}
		return capInstructions(meta.Instructions())
	}
	return ""
}

// maxServerInstructionsBytes caps one server's instructions in the prompt.
const maxServerInstructionsBytes = 3 << 10

func capInstructions(text string) string {
	if len(text) <= maxServerInstructionsBytes {
		return text
	}
	cut := text[:maxServerInstructionsBytes]
	// Back off to a rune boundary so the prompt never carries a broken rune.
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// ToolAllowed reports whether a loom account may see and call an exposed
// tool. A server with a users allowlist (ServerConfig.RestrictUsers) serves
// only those accounts; every other tool is open.
func (s *Service) ToolAllowed(p Principal, name string) bool {
	if s == nil {
		return false
	}
	server, _, ok := SplitExposedToolName(name)
	if !ok {
		return true
	}
	return s.ServerAllowed(p, server)
}

// ServerAllowed reports whether a loom account may see a server at all (its
// tools, its status in the /mcp panel).
func (s *Service) ServerAllowed(p Principal, server string) bool {
	if s == nil {
		return false
	}
	return s.cfg.Servers[server].userAllowed(p)
}
