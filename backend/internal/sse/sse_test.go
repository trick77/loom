package sse

import (
	"net/http/httptest"
	"strings"
	"testing"
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
