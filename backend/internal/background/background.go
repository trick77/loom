// Package background runs the goroutines that outlive the request which
// spawned them, and logs a panic in any goroutine outside the handler chain.
package background

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Group owns the goroutines a request spawns to outlive it: the project
// memory and description refreshes that run after a turn is delivered. It
// exists so shutdown can wait for them before the database closes, and so a
// panic in one of them is logged instead of taking the process down.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// mu guards stopped and orders every Add before Stop's Wait: an Add that
	// races a Wait on a drained group is a WaitGroup misuse panic.
	mu      sync.Mutex
	stopped bool
}

// New returns a group whose tasks stop when parent is done or Stop is called.
func New(parent context.Context) *Group {
	ctx, cancel := context.WithCancel(parent)
	return &Group{ctx: ctx, cancel: cancel}
}

// Spawn runs fn on its own goroutine. Its context keeps parent's values (the
// authenticated user, inference metadata) but not parent's cancellation: the
// request that spawned the task ending must not abort it, the group stopping
// must. A panic in fn is recovered and logged under label.
func (b *Group) Spawn(parent context.Context, label string, fn func(ctx context.Context)) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		slog.Warn("background task dropped: shutting down", "task", label)
		return
	}
	b.wg.Add(1)
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(b.ctx, cancel)
	go func() {
		defer b.wg.Done()
		defer stop()
		defer cancel()
		defer LogPanic(label)
		fn(ctx)
	}()
}

// Stop cancels every task and waits up to timeout for them to finish. A task
// that outlives the timeout is reported, not killed; the caller decides what
// that means for the resources the task may still hold.
func (b *Group) Stop(timeout time.Duration) error {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	// Drain first: a memory refresh that is mid model call gets to write its
	// result. Only what is still running at the deadline is cancelled, and it
	// gets a short grace to unwind before the caller closes the database.
	select {
	case <-done:
		b.cancel()
		return nil
	case <-time.After(timeout):
	}
	b.cancel()
	select {
	case <-done:
		return fmt.Errorf("background tasks did not finish within %v and were cancelled", timeout)
	case <-time.After(stopCancelGrace):
		return fmt.Errorf("background tasks still running after %v", timeout+stopCancelGrace)
	}
}

// stopCancelGrace is how long Stop waits for the tasks it had to cancel.
const stopCancelGrace = 2 * time.Second

// LogPanic is deferred by goroutines that run outside the HTTP handler chain,
// where the recovery middleware cannot catch a panic. It must be the deferred
// function itself (recover only works when called directly from one).
func LogPanic(label string) {
	if r := recover(); r != nil {
		slog.Error("recovered from panic in background task", "task", label, "panic", r, "stack", string(debug.Stack()))
	}
}
