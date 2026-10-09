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

func TestWriter_SendJSONEncodesThePayload(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}

	if err := w.SendJSON("error", map[string]string{"error": "a <b> & c"}); err != nil {
		t.Fatalf("SendJSON error: %v", err)
	}
	if got, want := rec.Body.String(), "event: error\ndata: {\"error\":\"a \\u003cb\\u003e \\u0026 c\"}\n\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	if err := w.SendJSON("bad", make(chan int)); err == nil {
		t.Error("SendJSON of an unencodable value returned no error")
	}
	if strings.Contains(rec.Body.String(), "event: bad") {
		t.Errorf("an unencodable value still wrote an event: %q", rec.Body.String())
	}
}

func TestWriter_setsAntiBufferingHeader(t *testing.T) {
	rec := httptest.NewRecorder()

	if _, err := NewWriter(rec); err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}

	// X-Accel-Buffering: no asks any fronting proxy / LB that honors the convention
	// (Traefik and others) not to buffer the response, so events reach the client as
	// they are sent.
	if v := rec.Header().Get("X-Accel-Buffering"); v != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", v)
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

// deadlineRecorder records the write deadline set through
// http.ResponseController and counts the writes made while one was set.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline        time.Time
	writesUnderTime int
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

func (d *deadlineRecorder) Write(p []byte) (int, error) {
	if !d.deadline.IsZero() {
		d.writesUnderTime++
	}
	return d.ResponseRecorder.Write(p)
}

// A client that stops reading (a frozen phone tab) must not block a write
// forever: each write runs under a deadline, cleared afterwards so it never
// outlives the stream on a kept-alive connection.
func TestWriter_SendWritesUnderADeadlineAndClearsIt(t *testing.T) {
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}
	if err := w.Send("ping", "1"); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if rec.writesUnderTime == 0 {
		t.Fatal("Send wrote without a write deadline")
	}
	if !rec.deadline.IsZero() {
		t.Fatalf("deadline = %v after Send, want cleared", rec.deadline)
	}
}
