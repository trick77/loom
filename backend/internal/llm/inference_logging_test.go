package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// allInferenceLogCapture records every inference log line, success or failure.
// (stream_idle_test.go's completedLogCapture keeps only the success attrs.)
type allInferenceLogCapture struct {
	mu    sync.Mutex
	lines []inferenceLogLine
}

type inferenceLogLine struct {
	message string
	attrs   map[string]slog.Value
}

func (h *allInferenceLogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *allInferenceLogCapture) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "llm inference ") {
		return nil
	}
	line := inferenceLogLine{message: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool { line.attrs[a.Key] = a.Value; return true })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, line)
	return nil
}
func (h *allInferenceLogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *allInferenceLogCapture) WithGroup(string) slog.Handler      { return h }

func (h *allInferenceLogCapture) only(t *testing.T) inferenceLogLine {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.lines) != 1 {
		t.Fatalf("captured %d inference log lines, want exactly 1: %+v", len(h.lines), h.lines)
	}
	return h.lines[0]
}

func captureAllInferenceLogs(t *testing.T) *allInferenceLogCapture {
	t.Helper()
	capture := &allInferenceLogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return capture
}

func completionServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The gate's log line is only diagnostic if it says what the gate decided —
// otherwise a mis-routed image turn looks identical to a correct one.
func TestClassifyImageIntent_logsTheDecision(t *testing.T) {
	srv := completionServer(t, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"create\",\"needs_text\":true}"},"finish_reason":"stop"}]}`)
	capture := captureAllInferenceLogs(t)

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{
		UserID: "user-1", Username: "jan", ThreadID: "thread-1", Purpose: "image_intent", Round: 1,
	})
	if _, err := c.ClassifyImageIntent(ctx, "erstelle ein Logo mit dem Wort Loom", false, false); err != nil {
		t.Fatalf("ClassifyImageIntent error = %v", err)
	}

	line := capture.only(t)
	if line.message != "llm inference completed" {
		t.Fatalf("message = %q, want %q", line.message, "llm inference completed")
	}
	if got := line.attrs["intent"].String(); got != "create" {
		t.Errorf("intent = %q, want create", got)
	}
	if !line.attrs["needs_text"].Bool() {
		t.Error("needs_text = false, want true")
	}
	if got := line.attrs["username"].String(); got != "jan" {
		t.Errorf("username = %q, want jan — the gate line must carry the same attribution as the chat line", got)
	}
	for key, value := range line.attrs {
		if strings.Contains(value.String(), "erstelle ein Logo") {
			t.Fatalf("attr %s leaked the user's message: %q", key, value.String())
		}
	}
}

// The classifier logs the category the turn actually uses, i.e. after the
// url_lookup coercion, not the model's raw reply.
func TestClassifyThread_logsTheCoercedCategory(t *testing.T) {
	srv := completionServer(t, `{"choices":[{"message":{"role":"assistant","content":"url_lookup"},"finish_reason":"stop"}]}`)
	capture := captureAllInferenceLogs(t)

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	category, err := c.ClassifyThread(context.Background(), "what did we decide yesterday?")
	if err != nil {
		t.Fatalf("ClassifyThread error = %v", err)
	}

	line := capture.only(t)
	if got := line.attrs["category"].String(); got != category {
		t.Errorf("logged category = %q, want the returned %q", got, category)
	}
	if got := line.attrs["category"].String(); got == "url_lookup" {
		t.Error("logged the model's raw reply instead of the coerced category")
	}
}

// The vision description runs from a detached ingest goroutine that attaches no
// metadata of its own; it must still be identifiable in the log stream.
func TestDescribeImage_logsWithADefaultPurpose(t *testing.T) {
	srv := completionServer(t, `{"choices":[{"message":{"role":"assistant","content":"A red bicycle."},"finish_reason":"stop"}]}`)
	capture := captureAllInferenceLogs(t)

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if _, err := c.DescribeImage(context.Background(), onePixelPNG(), "image/png"); err != nil {
		t.Fatalf("DescribeImage error = %v", err)
	}

	line := capture.only(t)
	if got := line.attrs["purpose"].String(); got != "image_describe" {
		t.Errorf("purpose = %q, want image_describe", got)
	}
	if got := line.attrs["model"].String(); got != visionModel {
		t.Errorf("model = %q, want %q", got, visionModel)
	}
}

// A caller-supplied purpose is the more specific one and must survive the
// client's default.
func TestDescribeImage_keepsACallerSuppliedPurpose(t *testing.T) {
	srv := completionServer(t, `{"choices":[{"message":{"role":"assistant","content":"A red bicycle."},"finish_reason":"stop"}]}`)
	capture := captureAllInferenceLogs(t)

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{UserID: "user-1", Purpose: "attachment_describe"})
	if _, err := c.DescribeImage(ctx, onePixelPNG(), "image/png"); err != nil {
		t.Fatalf("DescribeImage error = %v", err)
	}

	line := capture.only(t)
	if got := line.attrs["purpose"].String(); got != "attachment_describe" {
		t.Errorf("purpose = %q, want the caller's attachment_describe", got)
	}
	if got := line.attrs["user_id"].String(); got != "user-1" {
		t.Errorf("user_id = %q, want user-1", got)
	}
}
