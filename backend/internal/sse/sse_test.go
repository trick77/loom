package sse

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriter_writesEventAndData(t *testing.T) {
	rec := httptest.NewRecorder()

	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}
	if err := w.Send("ping", `{"n":1}`); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: ping\n") {
		t.Errorf("missing event line in %q", out)
	}
	if !strings.Contains(out, "data: {\"n\":1}\n\n") {
		t.Errorf("missing data line in %q", out)
	}
}

func TestWriter_HeartbeatEmitsCommentDuringSilence(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}

	stop := w.Heartbeat(context.Background(), 10*time.Millisecond)
	// Sleep many intervals so even a heavily loaded CI scheduler fires at least one
	// tick; slowness only yields MORE keepalives, never fewer, so the assertion is
	// one-sided and robust.
	time.Sleep(120 * time.Millisecond)
	stop() // synchronous: goroutine has exited, safe to read the buffer

	out := rec.Body.String()
	if !strings.Contains(out, ": keepalive\n\n") {
		t.Fatalf("expected a keepalive comment during silence, got %q", out)
	}
}

func TestWriter_HeartbeatStaysQuietWhileSending(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}

	// Interval far larger than the send cadence so no realistic scheduling jitter
	// can open a gap >= interval (which would emit a keepalive and fail this
	// assert-zero test). Each Send resets lastActivity, so as long as sends stay
	// well under 200ms apart, the heartbeat must stay quiet.
	stop := w.Heartbeat(context.Background(), 200*time.Millisecond)
	for i := 0; i < 10; i++ {
		if err := w.Send("ping", "x"); err != nil {
			t.Fatalf("Send error: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	stop()

	if n := strings.Count(rec.Body.String(), ": keepalive"); n != 0 {
		t.Fatalf("expected no keepalive while actively sending, got %d", n)
	}
}

func TestWriter_HeartbeatStopIsIdempotent(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}
	stop := w.Heartbeat(context.Background(), 10*time.Millisecond)
	stop()
	stop() // must not panic or block
}
