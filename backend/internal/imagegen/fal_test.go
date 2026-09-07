package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testFalModel     = "fal-ai/flux-2-pro"
	testFalSubmitURL = "/fal-ai/flux-2-pro"
	testFalEditURL   = "/fal-ai/flux-2-pro/edit"
)

// falStub stands in for fal's queue API: a submit endpoint per model path, the
// status document, the result document and the CDN delivery URL. Tests override
// the status and result bodies to drive the individual paths.
type falStub struct {
	t *testing.T
	// status is the queue status document returned for every poll.
	status map[string]any
	// result is the result document returned from the response URL.
	result map[string]any
	// image is the body served from the delivery URL, with its content type.
	image       []byte
	contentType string
	// pendingPolls holds the request IN_QUEUE for that many polls before the
	// configured status is served.
	pendingPolls int

	submitted map[string]any
	// submitPath is r.URL.Path (already unescaped); submitURI is the raw
	// request-target, which is what shows whether the model id was escaped.
	submitPath  string
	submitURI   string
	authHeaders []string
	pollCount   int
	cancel      context.CancelFunc
}

func newFalStub(t *testing.T) *falStub {
	t.Helper()
	return &falStub{
		t:           t,
		status:      map[string]any{"status": "COMPLETED"},
		image:       []byte("\x89PNG\r\n\x1a\nimage"),
		contentType: "image/png",
	}
}

func (s *falStub) start() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.authHeaders = append(s.authHeaders, r.Header.Get("authorization"))
		base := "http://" + r.Host
		switch r.URL.Path {
		case testFalSubmitURL, testFalEditURL, "/fal-ai/flux-2-max":
			s.submitPath = r.URL.Path
			s.submitURI = r.RequestURI
			if err := json.NewDecoder(r.Body).Decode(&s.submitted); err != nil {
				s.t.Fatalf("decode submit body: %v", err)
			}
			writeJSON(s.t, w, map[string]any{
				"request_id":   "req-1",
				"status_url":   base + "/queue/status",
				"response_url": base + "/queue/response",
			})
		case "/queue/status":
			s.pollCount++
			if s.cancel != nil {
				s.cancel()
			}
			if s.pollCount <= s.pendingPolls {
				writeJSON(s.t, w, map[string]any{"status": "IN_QUEUE", "queue_position": 1})
				return
			}
			writeJSON(s.t, w, s.status)
		case "/queue/response":
			result := s.result
			if result == nil {
				result = map[string]any{
					"images": []any{map[string]any{"url": base + "/delivery/image", "content_type": s.contentType}},
					"seed":   42,
				}
			}
			writeJSON(s.t, w, result)
		case "/delivery/image":
			w.Header().Set("Content-Type", s.contentType)
			_, _ = w.Write(s.image)
		default:
			s.t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	s.t.Cleanup(server.Close)
	return server
}

func (s *falStub) client(server *httptest.Server) *FalClient {
	return NewFalClient(FalConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        testFalModel,
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
}

func TestFalClientGenerateSubmitsPollsAndDownloadsImage(t *testing.T) {
	stub := newFalStub(t)
	server := stub.start()
	client := stub.client(server)

	result, err := client.Generate(context.Background(), GenerateRequest{
		Prompt:       "a small robot",
		Width:        512,
		Height:       512,
		OutputFormat: "png",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, header := range stub.authHeaders[:3] {
		if header != "Key test-key" {
			t.Fatalf("authorization header = %q, want %q on submit, status and result", header, "Key test-key")
		}
	}
	size, ok := stub.submitted["image_size"].(map[string]any)
	if !ok || size["width"].(float64) != 512 || size["height"].(float64) != 512 {
		t.Fatalf("image_size = %#v, want {width:512, height:512}", stub.submitted["image_size"])
	}
	if stub.submitted["prompt"] != "a small robot" || stub.submitted["output_format"] != "png" {
		t.Fatalf("submitted body = %#v", stub.submitted)
	}
	if result.RequestID != "req-1" || result.Provider != "fal" || result.MIMEType != "image/png" ||
		!strings.HasPrefix(string(result.Bytes), "\x89PNG") {
		t.Fatalf("result = %#v", result)
	}
	// fal reports no per-request price, so cost must stay unset rather than 0.
	if result.CostCredits != nil {
		t.Fatalf("CostCredits = %#v, want nil (fal returns no cost)", result.CostCredits)
	}
}

func TestFalClientGenerateJoinsSlashedModelIDIntoPath(t *testing.T) {
	stub := newFalStub(t)
	server := stub.start()
	client := stub.client(server)

	if _, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x", OutputFormat: "png"}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// A percent-escaped id ("fal-ai%2Fflux-2-pro") would 404 on fal. r.URL.Path
	// is already unescaped and so would look identical either way — the raw
	// request target is what actually distinguishes the two.
	if stub.submitURI != testFalSubmitURL {
		t.Fatalf("submit request target = %q, want %q (the model id must join the path unescaped)", stub.submitURI, testFalSubmitURL)
	}
}

func TestFalClientGenerateUsesPerRequestModelOverride(t *testing.T) {
	stub := newFalStub(t)
	server := stub.start()
	client := stub.client(server)

	result, err := client.Generate(context.Background(), GenerateRequest{
		Prompt:       "a bold LOOM wordmark",
		OutputFormat: "png",
		Model:        "fal-ai/flux-2-max",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if stub.submitPath != "/fal-ai/flux-2-max" {
		t.Fatalf("submit path = %q, want /fal-ai/flux-2-max", stub.submitPath)
	}
	if result.Model != "fal-ai/flux-2-max" {
		t.Fatalf("result.Model = %q, want fal-ai/flux-2-max (metadata must report the model actually used)", result.Model)
	}
}

func TestFalClientGenerateRoutesInputImagesToEditEndpoint(t *testing.T) {
	stub := newFalStub(t)
	server := stub.start()
	client := stub.client(server)

	primary := []byte("\x89PNG\r\n\x1a\nprimary-photo-bytes")
	secondary := []byte("\x89PNG\r\n\x1a\nsecond-reference-bytes")
	if _, err := client.Generate(context.Background(), GenerateRequest{
		Prompt:       "render this as a lego set",
		OutputFormat: "png",
		InputImages:  [][]byte{primary, secondary},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// Text-to-image and editing are separate endpoints on fal.
	if stub.submitPath != testFalEditURL {
		t.Fatalf("submit path = %q, want %q for a request carrying source images", stub.submitPath, testFalEditURL)
	}
	urls, ok := stub.submitted["image_urls"].([]any)
	if !ok || len(urls) != 2 {
		t.Fatalf("image_urls = %#v, want two entries", stub.submitted["image_urls"])
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(primary)
	if urls[0] != want {
		t.Fatalf("image_urls[0] = %#v, want a base64 data URI of the primary image", urls[0])
	}
}

func TestFalClientGenerateOmitsInputImagesWhenNone(t *testing.T) {
	stub := newFalStub(t)
	server := stub.start()
	client := stub.client(server)

	if _, err := client.Generate(context.Background(), GenerateRequest{Prompt: "a small robot", OutputFormat: "png"}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, ok := stub.submitted["image_urls"]; ok {
		t.Fatalf("image_urls present without source images: %#v", stub.submitted["image_urls"])
	}
	if stub.submitPath != testFalSubmitURL {
		t.Fatalf("submit path = %q, want the text-to-image endpoint %q", stub.submitPath, testFalSubmitURL)
	}
}

// fal rejects a side outside 256-2560 with a 422 the user would see as a raw
// tool error, while the shared validation allows down to 64 px and caps only the
// area — so the provider fits the request instead of forwarding it.
func TestFalClientGenerateFitsDimensionsToFalBounds(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		w, h                  int
		wantWidth, wantHeight int
	}{
		{"within bounds is untouched", 1024, 1024, 1024, 1024},
		{"tiny side is raised to the floor", 64, 64, 256, 256},
		{"long side scales down and keeps aspect ratio", 3840, 960, 2560, 640},
		// Scaling the long side down first is what keeps the floor bump from
		// pushing the area back over the cap.
		{"extreme ratio clamps both ends", 3840, 64, 2560, 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newFalStub(t)
			server := stub.start()
			client := stub.client(server)

			result, err := client.Generate(context.Background(), GenerateRequest{
				Prompt: "x", Width: tc.w, Height: tc.h, OutputFormat: "png",
			})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			size := stub.submitted["image_size"].(map[string]any)
			if int(size["width"].(float64)) != tc.wantWidth || int(size["height"].(float64)) != tc.wantHeight {
				t.Fatalf("image_size = %#v, want {width:%d, height:%d}", size, tc.wantWidth, tc.wantHeight)
			}
			// The reported metadata must match what was actually asked for.
			if result.Width != tc.wantWidth || result.Height != tc.wantHeight {
				t.Fatalf("result %dx%d, want %dx%d", result.Width, result.Height, tc.wantWidth, tc.wantHeight)
			}
		})
	}
}

func TestFalClientGenerateSendsSafetyToleranceAsString(t *testing.T) {
	for _, tc := range []struct {
		tolerance int
		want      string
		// fal's enum starts at 1, so the request's 0 clamps up to the strictest
		// level rather than being sent as an out-of-range "0".
	}{{0, "1"}, {1, "1"}, {2, "2"}, {5, "5"}} {
		stub := newFalStub(t)
		server := stub.start()
		client := stub.client(server)

		tolerance := tc.tolerance
		if _, err := client.Generate(context.Background(), GenerateRequest{
			Prompt:          "x",
			OutputFormat:    "png",
			SafetyTolerance: &tolerance,
		}); err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if got := stub.submitted["safety_tolerance"]; got != tc.want {
			t.Fatalf("safety_tolerance %d -> submitted %#v, want %q", tc.tolerance, got, tc.want)
		}
		// Turning the checker off requires an account authorization loom does not
		// assume, so the field must never be sent.
		if _, ok := stub.submitted["enable_safety_checker"]; ok {
			t.Fatalf("enable_safety_checker was sent: %#v", stub.submitted["enable_safety_checker"])
		}
	}
}

func TestFalClientGenerateReturnsValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":[{"msg":"field required"}]}`, http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	client := NewFalClient(FalConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        testFalModel,
		PollInterval: time.Millisecond,
		HTTPClient:   server.Client(),
	})
	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "fal submit failed") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestFalClientGenerateReturnsQueueError(t *testing.T) {
	stub := newFalStub(t)
	stub.status = map[string]any{"status": "IN_PROGRESS", "error": "runner crashed", "error_type": "InternalError"}
	server := stub.start()
	client := stub.client(server)

	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "fal generation failed: runner crashed") {
		t.Fatalf("Generate() error = %v, want the queue error surfaced", err)
	}
}

func TestFalClientGenerateReturnsContentPolicyError(t *testing.T) {
	stub := newFalStub(t)
	stub.status = map[string]any{"status": "IN_PROGRESS", "error_type": "content_policy_violation"}
	server := stub.start()
	client := stub.client(server)

	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "content policy") {
		t.Fatalf("Generate() error = %v, want a content policy error", err)
	}
}

func TestFalClientGenerateReturnsContentModeratedWhenResultIsNSFW(t *testing.T) {
	stub := newFalStub(t)
	stub.result = map[string]any{
		"images":            []any{map[string]any{"url": "http://unused/delivery/image"}},
		"has_nsfw_concepts": []any{true},
	}
	server := stub.start()
	client := stub.client(server)

	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "content moderated") {
		t.Fatalf("Generate() error = %v, want content moderated", err)
	}
}

func TestFalClientGenerateReturnsUnexpectedStatus(t *testing.T) {
	stub := newFalStub(t)
	stub.status = map[string]any{"status": "TASK_NOT_FOUND"}
	server := stub.start()
	client := stub.client(server)

	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("Generate() error = %v, want unexpected status", err)
	}
}

func TestFalClientGenerateTimesOutWhilePolling(t *testing.T) {
	stub := newFalStub(t)
	stub.status = map[string]any{"status": "IN_QUEUE", "queue_position": 3}
	server := stub.start()
	client := NewFalClient(FalConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        testFalModel,
		PollInterval: time.Millisecond,
		PollTimeout:  2 * time.Millisecond,
		HTTPClient:   server.Client(),
	})

	_, err := client.Generate(context.Background(), GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "fal generation timed out") {
		t.Fatalf("Generate() error = %v, want timeout", err)
	}
}

func TestFalClientGenerateReturnsCanceledWhenPollingContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := newFalStub(t)
	stub.status = map[string]any{"status": "IN_PROGRESS"}
	stub.cancel = cancel
	server := stub.start()
	client := NewFalClient(FalConfig{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		Model:        testFalModel,
		PollInterval: time.Millisecond,
		PollTimeout:  time.Minute,
		HTTPClient:   server.Client(),
	})

	_, err := client.Generate(ctx, GenerateRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "fal generation canceled") {
		t.Fatalf("Generate() error = %v, want canceled", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Generate() error = %v, did not want timeout wording", err)
	}
}

func TestFalClientGenerateUsesDownloadedWebPExtension(t *testing.T) {
	stub := newFalStub(t)
	stub.contentType = "image/webp"
	stub.image = []byte("RIFFxxxxWEBP")
	server := stub.start()
	client := stub.client(server)

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
