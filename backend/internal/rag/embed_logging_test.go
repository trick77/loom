package rag

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/inference"
)

// inferenceLogCapture records the inference log lines emitted during a test.
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

// The embedding model is a model call like any other, so it must show up in the
// same log stream as the chat and image calls — with the request's attribution
// and token counts, but never the text that was embedded.
func TestEmbedClient_Embed_logsCompletedInferenceWithoutInputs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"index": 0, "embedding": []float64{0.1}}},
			"usage": map[string]any{"prompt_tokens": 8, "total_tokens": 8},
		})
	}))
	defer srv.Close()
	capture := captureInferenceLogs(t)

	ctx := inference.WithMetadata(context.Background(), inference.Metadata{
		UserID: "user-1", Username: "jan", ThreadID: "thread-1", Purpose: "embed_query",
	})
	client := NewEmbedClient(EmbedConfig{BaseURL: srv.URL, Model: "embed-model"}, srv.Client())
	if _, err := client.Embed(ctx, []string{"a secret document chunk"}); err != nil {
		t.Fatalf("Embed() error: %v", err)
	}

	line := capture.only(t)
	if line.message != "llm inference completed" {
		t.Errorf("message = %q, want %q", line.message, "llm inference completed")
	}
	for key, want := range map[string]string{
		"model": "embed-model", "user_id": "user-1", "username": "jan",
		"thread_id": "thread-1", "purpose": "embed_query",
	} {
		if got := line.attrs[key].String(); got != want {
			t.Errorf("attr %s = %q, want %q", key, got, want)
		}
	}
	if got := line.attrs["input_count"].Int64(); got != 1 {
		t.Errorf("input_count = %d, want 1", got)
	}
	if got := line.attrs["total_tokens"].Int64(); got != 8 {
		t.Errorf("total_tokens = %d, want 8", got)
	}
	for key, value := range line.attrs {
		if strings.Contains(value.String(), "secret document chunk") {
			t.Fatalf("attr %s leaked the embedded text: %q", key, value.String())
		}
	}
}

// A failed embedding must still be accounted for: without this the only model
// call that could vanish from the logs entirely is the one that went wrong.
func TestEmbedClient_Embed_logsFailedInference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer srv.Close()
	capture := captureInferenceLogs(t)

	ctx := inference.WithMetadata(context.Background(), inference.Metadata{UserID: "user-1"})
	client := NewEmbedClient(EmbedConfig{BaseURL: srv.URL, Model: "embed-model"}, srv.Client())
	if _, err := client.Embed(ctx, []string{"chunk"}); err == nil {
		t.Fatal("Embed() succeeded, want error")
	}

	line := capture.only(t)
	if line.message != "llm inference failed" {
		t.Errorf("message = %q, want %q", line.message, "llm inference failed")
	}
	if got := line.attrs["user_id"].String(); got != "user-1" {
		t.Errorf("user_id = %q, want user-1", got)
	}
	if got := line.attrs["err"].String(); !strings.Contains(got, "upstream exploded") {
		t.Errorf("err = %q, want it to carry the upstream message", got)
	}
}

// An empty batch short-circuits before any request, so it must not fabricate a
// log line for a call that never happened.
func TestEmbedClient_Embed_emptyInputLogsNothing(t *testing.T) {
	capture := captureInferenceLogs(t)
	client := NewEmbedClient(EmbedConfig{BaseURL: "http://unused", Model: "embed-model"}, nil)
	if _, err := client.Embed(context.Background(), nil); err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(capture.lines) != 0 {
		t.Fatalf("captured %d log lines for an empty batch, want 0", len(capture.lines))
	}
}

// purposeCapturingEmbedder records the log purpose each embed call runs under.
type purposeCapturingEmbedder struct{ purposes []string }

func (e *purposeCapturingEmbedder) Embed(ctx context.Context, inputs []string) (EmbedResult, error) {
	e.purposes = append(e.purposes, inference.MetadataFromContext(ctx).Purpose)
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = unit()
	}
	return EmbedResult{Vectors: out}, nil
}

// Indexing batches and the per-turn query embedding have very different volumes
// and failure meanings, so they must be distinguishable in the logs.
func TestIngester_Ingest_tagsEmbedCallsAsIngest(t *testing.T) {
	emb := &purposeCapturingEmbedder{}
	ing, store := newIngester(t, fakeExtractor{text: strings.Repeat("word ", 5000)}, emb, fakeOpener{})
	ctx := context.Background()
	if err := store.CreateDocument(ctx, Document{
		ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt",
		MIME: "text/plain", Status: StatusPending,
	}); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if err := ing.Ingest(ctx, "u1", "d1"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(emb.purposes) == 0 {
		t.Fatal("no embed calls were made")
	}
	for i, purpose := range emb.purposes {
		if purpose != "embed_ingest" {
			t.Errorf("embed call %d purpose = %q, want embed_ingest", i, purpose)
		}
	}
}
