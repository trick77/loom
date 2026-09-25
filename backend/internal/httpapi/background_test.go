package httpapi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type bgKey struct{}

// Stop is a drain: tasks that finish on their own within the timeout are
// never cancelled, so an in-flight refresh gets to write its result.
func TestBackgroundStopDrainsTasksWithoutCancellingThem(t *testing.T) {
	bg := NewBackground(context.Background())
	var finished, cancelled atomic.Int32
	release := make(chan struct{})
	for range 2 {
		bg.Spawn(context.Background(), "task", func(ctx context.Context) {
			<-release
			if ctx.Err() != nil {
				cancelled.Add(1)
			}
			finished.Add(1)
		})
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(release)
	}()

	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := finished.Load(); got != 2 {
		t.Fatalf("finished tasks = %d, want 2", got)
	}
	if got := cancelled.Load(); got != 0 {
		t.Fatalf("cancelled tasks = %d, want 0", got)
	}
}

// What is still running at the deadline is cancelled and reported.
func TestBackgroundStopCancelsTasksStillRunningAtTheDeadline(t *testing.T) {
	bg := NewBackground(context.Background())
	var finished atomic.Int32
	bg.Spawn(context.Background(), "task", func(ctx context.Context) {
		<-ctx.Done()
		finished.Add(1)
	})

	if err := bg.Stop(20 * time.Millisecond); err == nil {
		t.Fatal("Stop() error = nil, want a report of the cancelled task")
	}
	if got := finished.Load(); got != 1 {
		t.Fatalf("finished tasks = %d, want 1 (cancelled and unwound)", got)
	}
}

func TestBackgroundStopReportsTasksThatIgnoreCancellation(t *testing.T) {
	bg := NewBackground(context.Background())
	release := make(chan struct{})
	bg.Spawn(context.Background(), "stuck", func(context.Context) { <-release })

	if err := bg.Stop(20 * time.Millisecond); err == nil {
		t.Fatal("Stop() error = nil, want a timeout for the stuck task")
	}
	close(release)
}

// A panic in a detached goroutine used to take the whole process down; the
// group logs it and keeps going, mirroring the recovery middleware.
func TestBackgroundRecoversPanic(t *testing.T) {
	bg := NewBackground(context.Background())
	bg.Spawn(context.Background(), "boom", func(context.Context) { panic("boom") })

	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// The task context keeps the request's values (auth user, inference metadata)
// but not its cancellation: the request ending must not abort the task, the
// group stopping must.
func TestBackgroundTaskKeepsParentValuesNotItsCancellation(t *testing.T) {
	bg := NewBackground(context.Background())
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), bgKey{}, "v"))
	seen := make(chan any, 1)
	canceledByParent := make(chan bool, 1)
	bg.Spawn(parent, "values", func(ctx context.Context) {
		seen <- ctx.Value(bgKey{})
		cancelParent()
		select {
		case <-ctx.Done():
			canceledByParent <- true
		case <-time.After(30 * time.Millisecond):
			canceledByParent <- false
		}
	})

	if v := <-seen; v != "v" {
		t.Fatalf("task saw value %v, want v", v)
	}
	if <-canceledByParent {
		t.Fatal("task was canceled by the parent request context")
	}
	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// A task spawned once Stop has begun (a stream handler outliving the HTTP
// shutdown reaches its post-turn refresh) is dropped: adding to the group
// while Stop waits on it would panic.
func TestBackgroundSpawnAfterStopIsDropped(t *testing.T) {
	bg := NewBackground(context.Background())
	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	ran := make(chan struct{}, 1)
	bg.Spawn(context.Background(), "late", func(context.Context) { ran <- struct{}{} })
	select {
	case <-ran:
		t.Fatal("a task spawned after Stop ran")
	case <-time.After(50 * time.Millisecond):
	}
}
