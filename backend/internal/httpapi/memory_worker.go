package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
)

// MemoryWorker runs the periodic, activity-gated memory refresh so generation no
// longer fires on every assistant turn. It sweeps every memoryBatchInterval and,
// for each user/project, only regenerates a memory that has new activity (a
// created or updated thread) and is past its debounce window: once a day for user
// memory, once an hour for project memory. Project memory is also refreshed
// responsively after a turn (see maybeRefreshProjectMemoryAsync); this sweep is
// the backstop that catches scopes whose last activity did not re-trigger that
// path, and it is the sole driver for user memory.
type MemoryWorker struct {
	s *server
}

// NewMemoryWorker builds the background memory worker from the same dependencies
// as the HTTP server.
func NewMemoryWorker(d Deps) *MemoryWorker {
	return &MemoryWorker{s: newServer(d)}
}

// Run sweeps every memoryBatchInterval until ctx is cancelled. Intended to be
// launched in its own goroutine at server startup with the process-lifetime
// context, so it stops cleanly on shutdown.
func (w *MemoryWorker) Run(ctx context.Context) {
	// An initial sweep shortly after boot populates memory without waiting a full
	// interval. This matters most for user memory, which — unlike project memory —
	// has no per-turn refresh path, so without it a process that restarts more
	// often than the interval could starve user memory indefinitely. The short
	// delay keeps it off the startup hot path.
	select {
	case <-ctx.Done():
		return
	case <-time.After(memoryBatchInitialDelay):
	}
	w.runOnce(ctx)

	ticker := time.NewTicker(memoryBatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// runOnce performs a single sweep across every user and their projects. Each
// scope is gated/debounced inside refreshMemoryIfDue, so an idle scope costs only
// a couple of cheap DB reads and never an LLM call. Every scope is isolated with
// recover so one panic (a store/LLM edge, a nil deref) cannot kill the long-lived
// worker goroutine and silently stop all future refreshes.
func (w *MemoryWorker) runOnce(ctx context.Context) {
	s := w.s
	if s.thread == nil || s.users == nil || s.llm == nil {
		return
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		slog.Warn("memory sweep: list users failed", "error", err)
		return
	}
	for _, user := range users {
		if ctx.Err() != nil {
			return
		}
		w.safely("user:"+user.ID, func() { w.refreshUserMemory(ctx, user) })
		w.safely("projects:"+user.ID, func() { w.refreshProjectMemories(ctx, user) })
	}
}

// safely runs fn, recovering from any panic so a single bad scope cannot abort
// the rest of the sweep or kill the worker goroutine. Mirrors the recovery
// middleware that protects the HTTP path.
func (w *MemoryWorker) safely(label string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("memory sweep: recovered from panic", "scope", label, "panic", r)
		}
	}()
	fn()
}

// refreshUserMemory refreshes one user's personal memory if it is due and stale
// (>= memoryUserRefreshAge since its last refresh).
func (w *MemoryWorker) refreshUserMemory(ctx context.Context, user auth.User) {
	rctx, cancel := context.WithTimeout(ctx, memoryBackgroundTimeout)
	defer cancel()
	if err := w.s.refreshMemoryIfDue(rctx, user, w.s.userMemoryScope(user), memoryUserRefreshAge); err != nil {
		slog.Warn("memory sweep: user memory refresh failed", "user_id", user.ID, "error", err)
	}
}

// refreshProjectMemories refreshes each of the user's (non-archived) projects'
// memory if due and stale (>= memoryProjectDebounce since its last refresh).
func (w *MemoryWorker) refreshProjectMemories(ctx context.Context, user auth.User) {
	projects, err := w.s.thread.ListProjects(ctx, user.ID, false)
	if err != nil {
		slog.Warn("memory sweep: list projects failed", "user_id", user.ID, "error", err)
		return
	}
	for _, project := range projects {
		if ctx.Err() != nil {
			return
		}
		w.safely("project:"+project.ID, func() { w.refreshProjectMemory(ctx, user, project) })
	}
}

func (w *MemoryWorker) refreshProjectMemory(ctx context.Context, user auth.User, project chat.Project) {
	rctx, cancel := context.WithTimeout(ctx, memoryBackgroundTimeout)
	defer cancel()
	if err := w.s.refreshMemoryIfDue(rctx, user, w.s.projectMemoryScope(user, project), memoryProjectDebounce); err != nil {
		slog.Warn("memory sweep: project memory refresh failed", "project_id", project.ID, "error", err)
	}
	// Backfill an empty description independently of the refresh gate above, which
	// is skipped for a project with no new messages — so a project missing its
	// description (e.g. one that was later cleared) self-heals on this sweep instead
	// of waiting for fresh activity. Cheap no-op once a description exists.
	w.s.maybeBackfillProjectDescription(rctx, user, project)
}
