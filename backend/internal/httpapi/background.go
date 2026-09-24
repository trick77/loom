package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Background owns the goroutines a request spawns to outlive it: the project
// memory and description refreshes that run after a turn is delivered. It
// exists so shutdown can wait for them before the database closes, and so a
// panic in one of them is logged instead of taking the process down.
type Background struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewBackground returns a group whose tasks stop when parent is done or Stop is
// called.
func NewBackground(parent context.Context) *Background {
	ctx, cancel := context.WithCancel(parent)
	return &Background{ctx: ctx, cancel: cancel}
}

// Spawn runs fn on its own goroutine. Its context keeps parent's values (the
// authenticated user, inference metadata) but not parent's cancellation: the
// request that spawned the task ending must not abort it, the group stopping
// must. A panic in fn is recovered and logged under label.
func (b *Background) Spawn(parent context.Context, label string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stop := context.AfterFunc(b.ctx, cancel)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer stop()
		defer cancel()
		defer logPanic(label)
		fn(ctx)
	}()
}

// Stop cancels every task and waits up to timeout for them to finish. A task
// that outlives the timeout is reported, not killed; the caller decides what
// that means for the resources the task may still hold.
func (b *Background) Stop(timeout time.Duration) error {
	b.cancel()
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("background tasks still running after %v", timeout)
	}
}

// logPanic is deferred by goroutines that run outside the HTTP handler chain,
// where the recovery middleware cannot catch a panic. It must be the deferred
// function itself (recover only works when called directly from one).
func logPanic(label string) {
	if r := recover(); r != nil {
		slog.Error("recovered from panic in background task", "task", label, "panic", r, "stack", string(debug.Stack()))
	}
}
