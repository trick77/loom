package imagegen

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/inference"
)

type inferenceLogCapture struct {
	mu    sync.Mutex
	lines []capturedLine
}

type capturedLine struct {
	message string
	attrs   map[string]slog.Value
}

func (h *inferenceLogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *inferenceLogCapture) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "llm inference ") {
		return nil
	}
	line := capturedLine{message: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool { line.attrs[a.Key] = a.Value; return true })
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, line)
	return nil
}
func (h *inferenceLogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *inferenceLogCapture) WithGroup(string) slog.Handler      { return h }

func (h *inferenceLogCapture) only(t *testing.T) capturedLine {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.lines) != 1 {
		t.Fatalf("captured %d inference log lines, want exactly 1: %+v", len(h.lines), h.lines)
	}
	return h.lines[0]
}

func captureInferenceLogs(t *testing.T) *inferenceLogCapture {
	t.Helper()
	capture := &inferenceLogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return capture
}

// Image generation is a model call and belongs in the same log stream as chat
// and embeddings: one line for the whole submit → poll → result → download round
// trip (not one per poll), carrying the request's attribution but never the prompt.
func TestFalClient_Generate_logsOneCompletedInferencePerCall(t *testing.T) {
	stub := newFalStub(t)
	// Sit in the queue for two polls first, so the single log line proves the
	// round trip is logged once rather than once per poll.
	stub.pendingPolls = 2
	server := stub.start()
	capture := captureInferenceLogs(t)

	client := stub.client(server)
	ctx := inference.WithMetadata(context.Background(), inference.Metadata{
		UserID: "user-1", Username: "jan", ThreadID: "thread-1",
	})
	if _, err := client.Generate(ctx, GenerateRequest{
		Prompt:       "a small secret robot",
		Width:        512,
		Height:       512,
		OutputFormat: "png",
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	line := capture.only(t)
	if line.message != "llm inference completed" {
		t.Errorf("message = %q, want %q", line.message, "llm inference completed")
	}
	for key, want := range map[string]string{
		"model": testFalModel, "user_id": "user-1", "username": "jan",
		"thread_id": "thread-1", "purpose": "image_generate", "request_id": "req-1",
	} {
		if got := line.attrs[key].String(); got != want {
			t.Errorf("attr %s = %q, want %q", key, got, want)
		}
	}
	if got := line.attrs["image_bytes"].Int64(); got == 0 {
		t.Error("image_bytes = 0, want the downloaded size")
	}
	for key, value := range line.attrs {
		if strings.Contains(value.String(), "secret robot") {
			t.Fatalf("attr %s leaked the prompt: %q", key, value.String())
		}
	}
}

// A provider failure is exactly the case someone greps for, so it must produce a
// failure line rather than nothing at all.
func TestFalClient_Generate_logsFailedInference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("provider down"))
	}))
	defer server.Close()
	capture := captureInferenceLogs(t)

	client := NewFalClient(FalConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        testFalModel,
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	ctx := inference.WithMetadata(context.Background(), inference.Metadata{UserID: "user-1"})
	if _, err := client.Generate(ctx, GenerateRequest{Prompt: "x", Width: 512, Height: 512, OutputFormat: "png"}); err == nil {
		t.Fatal("Generate() succeeded, want error")
	}

	line := capture.only(t)
	if line.message != "llm inference failed" {
		t.Errorf("message = %q, want %q", line.message, "llm inference failed")
	}
	if got := line.attrs["purpose"].String(); got != "image_generate" {
		t.Errorf("purpose = %q, want image_generate", got)
	}
	if got := line.attrs["user_id"].String(); got != "user-1" {
		t.Errorf("user_id = %q, want user-1", got)
	}
	if line.attrs["err"].String() == "" {
		t.Error("err attr is empty, want the provider error")
	}
}
