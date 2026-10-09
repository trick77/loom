package turn

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/llm"
)

func TestStreamFailureMessageMapsUserStalledAndGenericErrors(t *testing.T) {
	result := LoopResult{}
	if got := StreamFailureMessage(streamUserError{message: "image generation refused"}, result, "message", "t1"); got != "image generation refused" {
		t.Fatalf("user error -> %q", got)
	}
	if got := StreamFailureMessage(llm.ErrStreamStalled, result, "message", "t1"); got != llm.ErrStreamStalled.Error() {
		t.Fatalf("stalled -> %q", got)
	}
	if got := StreamFailureMessage(errors.New("boom"), result, "message", "t1"); got != "stream failed" {
		t.Fatalf("generic -> %q", got)
	}
}

func TestStreamFailureMessageLogsAGenericFailure(t *testing.T) {
	// A failed turn used to reach the client as "stream failed" and leave no
	// line in the log saying why.
	var log bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(restore) })

	StreamFailureMessage(errors.New("retrieve: disk I/O error"), LoopResult{StreamResult: llm.StreamResult{Content: "abc"}}, "message", "t1")

	got := log.String()
	for _, want := range []string{"level=ERROR", `msg="message stream failed"`, "thread_id=t1",
		`err="retrieve: disk I/O error"`, "content_bytes=3"} {
		if !strings.Contains(got, want) {
			t.Errorf("log = %q, want %q", got, want)
		}
	}
}

func TestStreamCanceled(t *testing.T) {
	// sqlite-vec reports an interrupt as "SQL logic error: chunks iter error",
	// not context.Canceled: a turn whose stream was cancelled is a cancel
	// whatever error it died on.
	interrupt := errors.New("vector search interrupted: sqlite3: SQL logic error: chunks iter error")
	live := context.Background()
	stopped, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("stream stop requested"))

	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"canceled error", live, context.Canceled, true},
		{"interrupt on a cancelled stream", stopped, interrupt, true},
		{"failure on a live stream", live, interrupt, false},
	} {
		if got := StreamCanceled(tc.ctx, tc.err); got != tc.want {
			t.Errorf("%s: StreamCanceled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMarshalTurnJSONFallsBackToEmptyArrays(t *testing.T) {
	trace, blocks := MarshalTurnJSON("t1", nil, nil)
	if string(trace) != "[]" || string(blocks) != "[]" {
		t.Fatalf("empty turn -> %s / %s, want [] / []", trace, blocks)
	}
	trace, blocks = MarshalTurnJSON("t1", []ActivityTraceEvent{{}}, []ContentBlock{{Type: "text", Content: "x"}})
	if string(trace) == "[]" || string(blocks) == "[]" {
		t.Fatalf("populated turn -> %s / %s, want encoded arrays", trace, blocks)
	}
}
