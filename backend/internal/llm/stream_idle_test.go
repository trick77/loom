package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The idle watchdog must abort a stream whose upstream goes silent mid-turn,
// reporting ErrStreamStalled (distinct from context.Canceled) and preserving
// whatever partial output arrived before the stall.
func TestClient_StreamIdleTimeoutAbortsStalledStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking hard\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Go silent: never send another chunk, never close [DONE].
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL:     server.URL,
		Model:       "mimo",
		Timeout:     5 * time.Second, // total budget far above the idle window
		IdleTimeout: 30 * time.Millisecond,
	}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err == nil || !errors.Is(err, ErrStreamStalled) {
		t.Fatalf("StreamChatResult() error = %v, want ErrStreamStalled", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("stall must not surface as context.Canceled (it would take the silent handler path): %v", err)
	}
	if !strings.Contains(result.ReasoningContent, "thinking hard") {
		t.Fatalf("partial reasoning lost on stall: %q", result.ReasoningContent)
	}
}

// As long as chunks keep arriving within the idle window the watchdog must not
// fire, even when the total stream spans more than one idle window.
func TestClient_StreamIdleTimeoutResetsOnEachChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Three 30ms gaps (90ms total) each stay under the 80ms idle window;
		// the run only completes if every chunk resets the watchdog.
		for i := 0; i < 3; i++ {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"tok \"}}]}\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL:     server.URL,
		Model:       "mimo",
		Timeout:     5 * time.Second,
		IdleTimeout: 80 * time.Millisecond,
	}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error = %v, want success", err)
	}
	if got := strings.TrimSpace(result.Content); got != "tok tok tok" {
		t.Fatalf("content = %q, want all three chunks", got)
	}
}

// SSE keep-alive comments (": ...") prove the connection is alive but not that the
// model is progressing; they must NOT reset the idle watchdog, otherwise a
// heartbeat-emitting upstream would mask a stalled model.
func TestClient_StreamIdleTimeoutIgnoresKeepAliveComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// Emit only keep-alive comments — no further data — until the request is
		// canceled. A working watchdog must still fire despite the heartbeats.
		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			time.Sleep(15 * time.Millisecond)
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL:     server.URL,
		Model:       "mimo",
		Timeout:     5 * time.Second,
		IdleTimeout: 60 * time.Millisecond,
	}, server.Client())

	_, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err == nil || !errors.Is(err, ErrStreamStalled) {
		t.Fatalf("StreamChatResult() error = %v, want ErrStreamStalled despite keep-alive comments", err)
	}
}

// A zero IdleTimeout disables the watchdog; the coarse total Timeout still bounds
// a silent stream (context deadline, not a stall).
func TestClient_StreamIdleTimeoutDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL:     server.URL,
		Model:       "mimo",
		Timeout:     30 * time.Millisecond,
		IdleTimeout: 0,
	}, server.Client())

	_, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StreamChatResult() error = %v, want context deadline exceeded", err)
	}
	if errors.Is(err, ErrStreamStalled) {
		t.Fatalf("watchdog must be disabled when IdleTimeout=0, got stall: %v", err)
	}
}
