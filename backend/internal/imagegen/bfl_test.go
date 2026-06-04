package imagegen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBFLClientGenerateSubmitsPollsAndDownloadsImage(t *testing.T) {
	var submitted map[string]any
	var sawKey bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flux-2-klein-4b":
			sawKey = r.Header.Get("x-key") == "test-key"
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatalf("decode submit body: %v", err)
			}
			writeJSON(t, w, map[string]any{
				"id":          "task-1",
				"polling_url": serverURL(r) + "/v1/get_result?id=task-1",
				"cost":        1.4,
			})
		case "/v1/get_result":
			writeJSON(t, w, map[string]any{
				"id":     "task-1",
				"status": "Ready",
				"result": map[string]any{"sample": serverURL(r) + "/delivery/image.png"},
			})
		case "/delivery/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nimage"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL + "/v1",
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	result, err := client.Generate(context.Background(), GenerateRequest{
		Prompt:       "a small robot",
		Width:        512,
		Height:       512,
		OutputFormat: "png",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !sawKey {
		t.Fatal("BFL x-key header was not sent")
	}
	if submitted["prompt"] != "a small robot" || submitted["width"].(float64) != 512 || submitted["height"].(float64) != 512 {
		t.Fatalf("submitted body = %#v", submitted)
	}
	if result.RequestID != "task-1" || result.MIMEType != "image/png" || !strings.HasPrefix(string(result.Bytes), "\x89PNG") {
		t.Fatalf("result = %#v", result)
	}
	if result.CostCredits == nil || *result.CostCredits != 1.4 {
		t.Fatalf("CostCredits = %#v", result.CostCredits)
	}
}

func TestBFLClientGenerateReturnsValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":[{"msg":"field required"}]}`, http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "BFL submit failed") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestBFLClientGenerateReturnsFailedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flux-2-klein-4b":
			writeJSON(t, w, map[string]any{
				"id":          "task-1",
				"polling_url": serverURL(r) + "/v1/get_result?id=task-1",
			})
		case "/v1/get_result":
			writeJSON(t, w, map[string]any{"id": "task-1", "status": "Failed"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL + "/v1",
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "BFL generation failed") {
		t.Fatalf("Generate() error = %v, want failed status", err)
	}
}

func TestBFLClientGenerateTimesOutWhilePolling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flux-2-klein-4b":
			writeJSON(t, w, map[string]any{
				"id":          "task-1",
				"polling_url": serverURL(r) + "/v1/get_result?id=task-1",
			})
		case "/v1/get_result":
			writeJSON(t, w, map[string]any{"id": "task-1", "status": "Processing"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL + "/v1",
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		PollTimeout:  2 * time.Millisecond,
		HTTPClient:   server.Client(),
	})
	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "BFL generation timed out") {
		t.Fatalf("Generate() error = %v, want timeout", err)
	}
}

func TestBFLClientGenerateReturnsCanceledWhenPollingContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flux-2-klein-4b":
			writeJSON(t, w, map[string]any{
				"id":          "task-1",
				"polling_url": serverURL(r) + "/v1/get_result?id=task-1",
			})
		case "/v1/get_result":
			cancel()
			writeJSON(t, w, map[string]any{"id": "task-1", "status": "Processing"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL + "/v1",
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		PollTimeout:  time.Minute,
		HTTPClient:   server.Client(),
	})
	_, err := client.Generate(ctx, GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "BFL generation canceled") {
		t.Fatalf("Generate() error = %v, want canceled", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Generate() error = %v, did not want timeout wording", err)
	}
}

func TestBFLClientGenerateUsesDownloadedWebPExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flux-2-klein-4b":
			writeJSON(t, w, map[string]any{
				"id":          "task-1",
				"polling_url": serverURL(r) + "/v1/get_result?id=task-1",
			})
		case "/v1/get_result":
			writeJSON(t, w, map[string]any{
				"id":     "task-1",
				"status": "Ready",
				"result": map[string]any{"sample": serverURL(r) + "/delivery/image.webp"},
			})
		case "/delivery/image.webp":
			w.Header().Set("Content-Type", "image/webp")
			_, _ = w.Write([]byte("RIFFxxxxWEBP"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBFLClient(BFLConfig{
		BaseURL:      server.URL + "/v1",
		APIKey:       "test-key",
		Model:        "flux-2-klein-4b",
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	result, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x", OutputFormat: "png"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.MIMEType != "image/webp" || result.Extension != "webp" {
		t.Fatalf("result MIME/extension = %s/%s, want image/webp/webp", result.MIMEType, result.Extension)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
