package httpapi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type bgKey struct{}

func TestBackgroundStopCancelsAndWaitsForTasks(t *testing.T) {
	bg := NewBackground(context.Background())
	var finished atomic.Int32
	for range 2 {
		bg.Spawn(context.Background(), "task", func(ctx context.Context) {
			<-ctx.Done()
			finished.Add(1)
		})
	}

	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := finished.Load(); got != 2 {
		t.Fatalf("finished tasks = %d, want 2", got)
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
		<-ctx.Done()
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
