# FLUX Klein Image Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add first-class text-to-image generation to Lume using the BFL FLUX.2 [klein] API, saving generated images as normal per-user/project artifacts and rendering them inline in chat.

**Architecture:** Implement a small `imagegen` backend package with a provider interface and a BFL client. Expose the provider as a built-in LLM tool named `generate_image`, then reuse the existing tool loop, SSE `artifact` event, artifact storage, sandboxed output paths, and assistant message artifact persistence. The frontend only needs to render image artifacts as previews; explicit image compose mode and image editing are deferred.

**Tech Stack:** Go stdlib `net/http`, BFL API `POST https://api.bfl.ai/v1/flux-2-klein-4b` with `x-key`, existing Lume SSE/tool/artifact pipeline, React + TypeScript.

**Current BFL docs checked:** Context7 `/websites/bfl_ai`, query “FLUX.2 [klein] text-to-image API”. Relevant docs: `flux_2/flux2_text_to_image`, `api-reference/models/generate-or-edit-an-image-with-flux2-[klein-9b]-fast-editing`, `api_integration/integration_guidelines`.

---

## File Structure

- **Create** `backend/internal/imagegen/model.go` — provider-neutral request/result types, tool schema, and validation helpers.
- **Create** `backend/internal/imagegen/bfl.go` — BFL FLUX.2 [klein] client: submit, poll, download generated image bytes.
- **Create** `backend/internal/imagegen/bfl_test.go` — httptest coverage for request payload, polling, image download, and API errors.
- **Create** `backend/internal/imagegen/tool.go` — built-in tool wrapper that generates an image and writes it to an artifact.
- **Create** `backend/internal/imagegen/tool_test.go` — tool-level tests for artifact bytes, filenames, formats, and validation.
- **Modify** `backend/internal/config/config.go` — add image generation env vars.
- **Modify** `backend/internal/config/config_test.go` — config defaults and required-key behavior.
- **Modify** `backend/cmd/slopr/main.go` — instantiate BFL client/tool when configured.
- **Modify** `backend/internal/httpapi/server.go` — accept image tools in dependencies.
- **Modify** `backend/internal/httpapi/message_stream_handlers.go` — include and execute image tools alongside document tools.
- **Modify** `backend/internal/httpapi/message_stream_handlers_test.go` — verify image tool emits artifact event and persists artifact metadata.
- **Modify** `backend/internal/artifact/model.go` — return correct MIME types for `png`, `jpg`, `jpeg`, `webp`.
- **Modify** `backend/internal/artifact/path_test.go` — verify image MIME type mapping.
- **Modify** `.env.example` — document BFL image generation config without committing a real key.
- **Modify** `frontend/src/ChatShell.tsx` — show inline previews for image artifacts.
- **Modify** `frontend/src/api.test.ts` or add focused frontend test if existing coverage can exercise image artifact rendering.

---

### Task 1: Add Image Artifact MIME Types

**Files:**
- Modify: `backend/internal/artifact/model.go`
- Modify: `backend/internal/artifact/path_test.go`

- [ ] **Step 1: Write the failing MIME type test**

Append this test to `backend/internal/artifact/path_test.go`:

```go
func TestMIMETypeImages(t *testing.T) {
	tests := map[string]string{
		"png":  "image/png",
		".jpg": "image/jpeg",
		"jpeg": "image/jpeg",
		"webp": "image/webp",
	}
	for extension, want := range tests {
		if got := MIMEType(extension); got != want {
			t.Fatalf("MIMEType(%q) = %q, want %q", extension, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/artifact -run TestMIMETypeImages`

Expected: FAIL because `MIMEType("png")` currently falls back to `text/plain; charset=utf-8`.

- [ ] **Step 3: Add image MIME mappings**

In `backend/internal/artifact/model.go`, update `MIMEType`:

```go
func MIMEType(extension string) string {
	switch normalizeExtension(extension) {
	case "pdf":
		return "application/pdf"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "csv":
		return "text/csv; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	case "md":
		return "text/markdown; charset=utf-8"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "text/plain; charset=utf-8"
	}
}
```

- [ ] **Step 4: Run artifact tests**

Run: `cd backend && go test ./internal/artifact`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/artifact/model.go backend/internal/artifact/path_test.go
git commit -m "feat: recognize image artifact mime types"
```

---

### Task 2: Add Image Generation Config

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `.env.example`

- [ ] **Step 1: Write failing config tests**

Add these tests to `backend/internal/config/config_test.go`:

```go
func TestLoadImageGenerationDefaultsDisabled(t *testing.T) {
	t.Setenv("SLOPR_SESSION_SECRET", "secret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLAPIKey != "" {
		t.Fatal("BFLAPIKey default was not empty")
	}
	if cfg.BFLBaseURL != "https://api.bfl.ai/v1" {
		t.Fatalf("BFLBaseURL = %q", cfg.BFLBaseURL)
	}
	if cfg.BFLModel != "flux-2-klein-4b" {
		t.Fatalf("BFLModel = %q", cfg.BFLModel)
	}
}

func TestLoadBFLImageRequiresBaseURLWhenAPIKeyIsSet(t *testing.T) {
	t.Setenv("SLOPR_SESSION_SECRET", "secret")
	t.Setenv("SLOPR_BFL_API_KEY", "bfl-test")
	t.Setenv("SLOPR_BFL_BASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SLOPR_BFL_BASE_URL is required") {
		t.Fatalf("Load() error = %v, want SLOPR_BFL_BASE_URL required", err)
	}
}

func TestLoadBFLImageConfiguredByAPIKey(t *testing.T) {
	t.Setenv("SLOPR_SESSION_SECRET", "secret")
	t.Setenv("SLOPR_BFL_API_KEY", "bfl-test")
	t.Setenv("SLOPR_BFL_MODEL", "flux-2-klein-9b")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLAPIKey != "bfl-test" {
		t.Fatalf("BFLAPIKey was not loaded")
	}
	if cfg.BFLModel != "flux-2-klein-9b" {
		t.Fatalf("BFLModel = %q", cfg.BFLModel)
	}
}
```

If `strings` is not already imported in `config_test.go`, add it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/config -run 'TestLoad(Image|BFL)'`

Expected: FAIL because `Config` has no image generation fields.

- [ ] **Step 3: Add config fields and validation**

In `backend/internal/config/config.go`, add fields to `Config` after the embedding fields:

```go
	BFLBaseURL string
	BFLAPIKey  string
	BFLModel   string
```

Add defaults after `EmbedModel`:

```go
		BFLBaseURL:           env("SLOPR_BFL_BASE_URL", "https://api.bfl.ai/v1"),
		BFLAPIKey:            env("SLOPR_BFL_API_KEY", ""),
		BFLModel:             env("SLOPR_BFL_MODEL", "flux-2-klein-4b"),
```

After the existing Context7 validation, add:

```go
	if cfg.BFLAPIKey != "" {
		if cfg.BFLBaseURL == "" {
			return Config{}, fmt.Errorf("SLOPR_BFL_BASE_URL is required when SLOPR_BFL_API_KEY is set")
		}
		if cfg.BFLModel == "" {
			return Config{}, fmt.Errorf("SLOPR_BFL_MODEL is required when SLOPR_BFL_API_KEY is set")
		}
	}
```

- [ ] **Step 4: Update `.env.example`**

Add this block after the embedding settings:

```dotenv
# Image generation (optional). Default model is FLUX.2 [klein] 4B.
SLOPR_BFL_BASE_URL=https://api.bfl.ai/v1
SLOPR_BFL_API_KEY=
SLOPR_BFL_MODEL=flux-2-klein-4b
```

- [ ] **Step 5: Run config tests**

Run: `cd backend && go test ./internal/config`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go .env.example
git commit -m "feat: add image generation configuration"
```

---

### Task 3: Add Provider-Neutral Image Generation Types

**Files:**
- Create: `backend/internal/imagegen/model.go`
- Create: `backend/internal/imagegen/model_test.go`

- [ ] **Step 1: Create tests for request normalization**

Create `backend/internal/imagegen/model_test.go`:

```go
package imagegen

import "testing"

func TestGenerateRequestNormalize(t *testing.T) {
	req := GenerateRequest{
		Prompt:       "  a clay robot reading a book  ",
		Filename:     "robot",
		Width:        1000,
		Height:       777,
		OutputFormat: "",
	}
	got, err := req.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Prompt != "a clay robot reading a book" {
		t.Fatalf("Prompt = %q", got.Prompt)
	}
	if got.Width != 1008 || got.Height != 784 {
		t.Fatalf("dimensions = %dx%d, want 1008x784", got.Width, got.Height)
	}
	if got.OutputFormat != "png" {
		t.Fatalf("OutputFormat = %q", got.OutputFormat)
	}
	if got.Filename != "robot.png" {
		t.Fatalf("Filename = %q", got.Filename)
	}
}

func TestGenerateRequestNormalizeRejectsEmptyPrompt(t *testing.T) {
	_, err := (GenerateRequest{Prompt: "   "}).Normalized()
	if err == nil {
		t.Fatal("Normalized() succeeded, want error")
	}
}

func TestGenerateRequestNormalizeRejectsUnsupportedFormat(t *testing.T) {
	_, err := (GenerateRequest{Prompt: "test", OutputFormat: "gif"}).Normalized()
	if err == nil {
		t.Fatal("Normalized() succeeded, want error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/imagegen`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Add model types and validation**

Create `backend/internal/imagegen/model.go`:

```go
package imagegen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	DefaultWidth        = 1024
	DefaultHeight       = 1024
	DefaultOutputFormat = "png"
	MaxPromptRunes      = 4000
	MaxOutputPixels     = 4_000_000
)

type GenerateRequest struct {
	Prompt          string
	Filename        string
	Width           int
	Height          int
	Seed            *int64
	OutputFormat    string
	SafetyTolerance *int
}

type GenerateResult struct {
	Filename    string
	Extension   string
	MIMEType    string
	Bytes       []byte
	Provider    string
	Model       string
	RequestID   string
	Prompt      string
	Seed        *int64
	Width       int
	Height      int
	CostCredits *float64
}

type Provider interface {
	Generate(context.Context, GenerateRequest) (GenerateResult, error)
}

func (r GenerateRequest) Normalized() (GenerateRequest, error) {
	out := r
	out.Prompt = strings.TrimSpace(out.Prompt)
	if out.Prompt == "" {
		return GenerateRequest{}, errors.New("prompt is required")
	}
	if len([]rune(out.Prompt)) > MaxPromptRunes {
		return GenerateRequest{}, fmt.Errorf("prompt must be at most %d characters", MaxPromptRunes)
	}
	if out.Width == 0 {
		out.Width = DefaultWidth
	}
	if out.Height == 0 {
		out.Height = DefaultHeight
	}
	out.Width = align16(out.Width)
	out.Height = align16(out.Height)
	if out.Width < 64 || out.Height < 64 {
		return GenerateRequest{}, errors.New("width and height must be at least 64 pixels")
	}
	if out.Width*out.Height > MaxOutputPixels {
		return GenerateRequest{}, errors.New("width and height must not exceed 4 megapixels")
	}
	out.OutputFormat = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(out.OutputFormat), "."))
	if out.OutputFormat == "" {
		out.OutputFormat = DefaultOutputFormat
	}
	if out.OutputFormat != "png" && out.OutputFormat != "jpeg" && out.OutputFormat != "jpg" {
		return GenerateRequest{}, errors.New("output_format must be png or jpeg")
	}
	if out.OutputFormat == "jpg" {
		out.OutputFormat = "jpeg"
	}
	if out.SafetyTolerance == nil {
		out.SafetyTolerance = intPtr(2)
	}
	if *out.SafetyTolerance < 0 || *out.SafetyTolerance > 5 {
		return GenerateRequest{}, errors.New("safety_tolerance must be between 0 and 5")
	}
	out.Filename = normalizeFilename(out.Filename, out.OutputFormat)
	return out, nil
}

func align16(v int) int {
	if v%16 == 0 {
		return v
	}
	return v + (16 - v%16)
}

func normalizeFilename(input, format string) string {
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	name := strings.TrimSpace(input)
	if name == "" {
		name = "generated-image"
	}
	name = filepath.Base(name)
	if current := filepath.Ext(name); current != "" {
		name = strings.TrimSuffix(name, current)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "generated-image"
	}
	return name + "." + ext
}

func MIMEType(format string) string {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
```

- [ ] **Step 4: Run imagegen model tests**

Run: `cd backend && go test ./internal/imagegen -run TestGenerateRequest`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/imagegen/model.go backend/internal/imagegen/model_test.go
git commit -m "feat: add image generation request model"
```

---

### Task 4: Implement BFL FLUX.2 Klein Client

**Files:**
- Create: `backend/internal/imagegen/bfl.go`
- Create: `backend/internal/imagegen/bfl_test.go`

- [ ] **Step 1: Write BFL client tests**

Create `backend/internal/imagegen/bfl_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/imagegen -run TestBFLClient`

Expected: FAIL because `NewBFLClient` is undefined.

- [ ] **Step 3: Implement the BFL client**

Create `backend/internal/imagegen/bfl.go`:

```go
package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBFLPollInterval = 750 * time.Millisecond
	defaultBFLPollTimeout  = 2 * time.Minute
)

type BFLConfig struct {
	BaseURL      string
	APIKey       string
	Model        string
	PollInterval time.Duration
	PollTimeout  time.Duration
	HTTPClient   *http.Client
}

type BFLClient struct {
	baseURL      string
	apiKey       string
	model        string
	pollInterval time.Duration
	pollTimeout  time.Duration
	httpClient   *http.Client
}

func NewBFLClient(cfg BFLConfig) *BFLClient {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultBFLPollInterval
	}
	pollTimeout := cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultBFLPollTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &BFLClient{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
		model:        strings.Trim(strings.TrimSpace(cfg.Model), "/"),
		pollInterval: pollInterval,
		pollTimeout:  pollTimeout,
		httpClient:   client,
	}
}

func (c *BFLClient) Generate(ctx context.Context, input GenerateRequest) (GenerateResult, error) {
	req, err := input.Normalized()
	if err != nil {
		return GenerateResult{}, err
	}
	submitted, err := c.submit(ctx, req)
	if err != nil {
		return GenerateResult{}, err
	}
	status, err := c.poll(ctx, submitted.PollingURL)
	if err != nil {
		return GenerateResult{}, err
	}
	imageURL := strings.TrimSpace(status.Result.Sample)
	if imageURL == "" {
		return GenerateResult{}, fmt.Errorf("BFL result did not include an image URL")
	}
	body, contentType, err := c.download(ctx, imageURL)
	if err != nil {
		return GenerateResult{}, err
	}
	mimeType := MIMEType(req.OutputFormat)
	if strings.HasPrefix(contentType, "image/") {
		mimeType = strings.Split(contentType, ";")[0]
	}
	return GenerateResult{
		Filename:    req.Filename,
		Extension:   extensionForMIME(mimeType, req.OutputFormat),
		MIMEType:    mimeType,
		Bytes:       body,
		Provider:    "bfl",
		Model:       c.model,
		RequestID:   submitted.ID,
		Prompt:      req.Prompt,
		Seed:        req.Seed,
		Width:       req.Width,
		Height:      req.Height,
		CostCredits: submitted.Cost,
	}, nil
}

func (c *BFLClient) submit(ctx context.Context, req GenerateRequest) (bflSubmitResponse, error) {
	payload := map[string]any{
		"prompt":           req.Prompt,
		"width":            req.Width,
		"height":           req.Height,
		"safety_tolerance": *req.SafetyTolerance,
		"output_format":    req.OutputFormat,
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return bflSubmitResponse{}, err
	}
	endpoint := c.baseURL + "/" + url.PathEscape(c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return bflSubmitResponse{}, err
	}
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-key", c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return bflSubmitResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return bflSubmitResponse{}, fmt.Errorf("BFL submit failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out bflSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return bflSubmitResponse{}, err
	}
	if out.ID == "" || out.PollingURL == "" {
		return bflSubmitResponse{}, fmt.Errorf("BFL submit response missing id or polling_url")
	}
	return out, nil
}

func (c *BFLClient) poll(ctx context.Context, pollingURL string) (bflStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.pollTimeout)
	defer cancel()
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		status, err := c.fetchStatus(ctx, pollingURL)
		if err != nil {
			return bflStatusResponse{}, err
		}
		switch strings.ToLower(status.Status) {
		case "ready", "completed", "succeeded":
			return status, nil
		case "error", "failed":
			return bflStatusResponse{}, fmt.Errorf("BFL generation failed")
		}
		select {
		case <-ctx.Done():
			return bflStatusResponse{}, fmt.Errorf("BFL generation timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *BFLClient) fetchStatus(ctx context.Context, pollingURL string) (bflStatusResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pollingURL, nil)
	if err != nil {
		return bflStatusResponse{}, err
	}
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("x-key", c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return bflStatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return bflStatusResponse{}, fmt.Errorf("BFL poll failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out bflStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return bflStatusResponse{}, err
	}
	return out, nil
}

func (c *BFLClient) download(ctx context.Context, imageURL string) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download generated image failed: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > 25<<20 {
		return nil, "", fmt.Errorf("generated image is too large")
	}
	return body, resp.Header.Get("content-type"), nil
}

type bflSubmitResponse struct {
	ID         string   `json:"id"`
	PollingURL string   `json:"polling_url"`
	Cost       *float64 `json:"cost"`
}

type bflStatusResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result struct {
		Sample string `json:"sample"`
	} `json:"result"`
}

func extensionForMIME(mimeType, fallbackFormat string) string {
	switch mimeType {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	default:
		if fallbackFormat == "jpeg" {
			return "jpg"
		}
		return fallbackFormat
	}
}
```

- [ ] **Step 4: Run imagegen tests**

Run: `cd backend && go test ./internal/imagegen`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/imagegen/bfl.go backend/internal/imagegen/bfl_test.go
git commit -m "feat: add BFL image generation client"
```

---

### Task 5: Add Built-In Image Generation Tool

**Files:**
- Create: `backend/internal/imagegen/tool.go`
- Create: `backend/internal/imagegen/tool_test.go`

- [ ] **Step 1: Write tool tests**

Create `backend/internal/imagegen/tool_test.go`:

```go
package imagegen

import (
	"bytes"
	"context"
	"testing"
)

type fakeProvider struct {
	req GenerateRequest
}

func (f *fakeProvider) Generate(_ context.Context, req GenerateRequest) (GenerateResult, error) {
	f.req = req
	return GenerateResult{
		Filename:  req.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("png-bytes"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    req.Prompt,
		Width:     req.Width,
		Height:    req.Height,
	}, nil
}

func TestToolSchema(t *testing.T) {
	tool := NewTool(&fakeProvider{})
	schema := tool.Schema()
	if schema.Name != "generate_image" {
		t.Fatalf("Name = %q", schema.Name)
	}
	if schema.Parameters["type"] != "object" {
		t.Fatalf("Parameters = %#v", schema.Parameters)
	}
}

func TestToolGenerateWritesImage(t *testing.T) {
	provider := &fakeProvider{}
	tool := NewTool(provider)
	var out bytes.Buffer
	meta, err := tool.Generate(context.Background(), ToolRequest{
		Prompt:   "a robot",
		Filename: "robot",
		Width:    512,
		Height:   512,
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if out.String() != "png-bytes" {
		t.Fatalf("written bytes = %q", out.String())
	}
	if meta.DisplayFilename != "robot.png" || meta.Extension != "png" || meta.MIMEType != "image/png" {
		t.Fatalf("meta = %#v", meta)
	}
	if provider.req.Prompt != "a robot" || provider.req.Width != 512 {
		t.Fatalf("provider request = %#v", provider.req)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/imagegen -run TestTool`

Expected: FAIL because `NewTool` is undefined.

- [ ] **Step 3: Implement the tool wrapper**

Create `backend/internal/imagegen/tool.go`:

```go
package imagegen

import (
	"context"
	"fmt"
	"io"
)

type Tool struct {
	provider Provider
}

type ToolRequest struct {
	Prompt          string `json:"prompt"`
	Filename        string `json:"filename,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	Seed            *int64 `json:"seed,omitempty"`
	OutputFormat    string `json:"output_format,omitempty"`
	SafetyTolerance *int    `json:"safety_tolerance,omitempty"`
}

type ToolMeta struct {
	DisplayFilename string
	Extension       string
	MIMEType        string
	Provider        string
	Model           string
	RequestID       string
}

func NewTool(provider Provider) Tool {
	return Tool{provider: provider}
}

func (t Tool) ToolName() string {
	return "generate_image"
}

func (t Tool) Schema() ToolSchema {
	return ToolSchema{
		Name:        t.ToolName(),
		Description: "Generate a PNG or JPEG image from a text prompt. Use this when the user asks to create, draw, render, or generate an image.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Detailed visual prompt for the generated image.",
				},
				"filename": map[string]any{
					"type":        "string",
					"description": "Optional output filename without path. The extension is added automatically.",
				},
				"width": map[string]any{
					"type":        "integer",
					"description": "Output width in pixels. Defaults to 1024. Rounded up to a multiple of 16.",
				},
				"height": map[string]any{
					"type":        "integer",
					"description": "Output height in pixels. Defaults to 1024. Rounded up to a multiple of 16.",
				},
				"seed": map[string]any{
					"type":        "integer",
					"description": "Optional seed for reproducible generation.",
				},
				"output_format": map[string]any{
					"type":        "string",
					"enum":        []string{"png", "jpeg"},
					"description": "Output image format. Defaults to png.",
				},
				"safety_tolerance": map[string]any{
					"type":        "integer",
					"description": "BFL safety tolerance from 0 to 5. Defaults to 2.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t Tool) Generate(ctx context.Context, req ToolRequest, w io.Writer) (ToolMeta, error) {
	if t.provider == nil {
		return ToolMeta{}, fmt.Errorf("BFL image generation is not configured")
	}
	result, err := t.provider.Generate(ctx, GenerateRequest{
		Prompt:          req.Prompt,
		Filename:        req.Filename,
		Width:           req.Width,
		Height:          req.Height,
		Seed:            req.Seed,
		OutputFormat:    req.OutputFormat,
		SafetyTolerance: req.SafetyTolerance,
	})
	if err != nil {
		return ToolMeta{}, err
	}
	if _, err := w.Write(result.Bytes); err != nil {
		return ToolMeta{}, err
	}
	return ToolMeta{
		DisplayFilename: result.Filename,
		Extension:       result.Extension,
		MIMEType:        result.MIMEType,
		Provider:        result.Provider,
		Model:           result.Model,
		RequestID:       result.RequestID,
	}, nil
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}
```

- [ ] **Step 4: Run imagegen tests**

Run: `cd backend && go test ./internal/imagegen`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/imagegen/tool.go backend/internal/imagegen/tool_test.go
git commit -m "feat: add image generation tool wrapper"
```

---

### Task 6: Wire Image Tool Into the Chat Tool Loop

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/message_stream_handlers.go`
- Modify: `backend/internal/httpapi/message_stream_handlers_test.go`

- [ ] **Step 1: Add failing HTTP stream test**

Add a test to `backend/internal/httpapi/message_stream_handlers_test.go` modeled after the existing document artifact stream test. Use a fake image tool that returns PNG bytes and an LLM fake that calls `generate_image`.

The assertion must check:

```go
if !strings.Contains(rec.Body.String(), "event: artifact") {
	t.Fatalf("stream missing artifact event:\n%s", rec.Body.String())
}
if !strings.Contains(rec.Body.String(), "image/png") {
	t.Fatalf("stream missing image mime type:\n%s", rec.Body.String())
}
assistant := chatStore.messages[len(chatStore.messages)-1]
if !bytes.Contains(assistant.Artifacts, []byte("image/png")) {
	t.Fatalf("assistant artifacts = %s", assistant.Artifacts)
}
```

Use the existing test helpers in `backend/internal/httpapi/chat_test_helpers_test.go`; do not introduce real BFL calls.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/httpapi -run TestStreamMessageImageToolCreatesArtifact`

Expected: FAIL because the server has no image tool dependency.

- [ ] **Step 3: Add image tool dependency to server**

In `backend/internal/httpapi/server.go`, import `github.com/trick77/lume/internal/imagegen`.

Add to `Deps`:

```go
	ImageTools            []imagegen.Tool
```

Add to `server`:

```go
	imageTools            []imagegen.Tool
```

Set it in `New`:

```go
		imageTools:            d.ImageTools,
```

- [ ] **Step 4: Include image tools in available tool schemas**

In `backend/internal/httpapi/message_stream_handlers.go`, import `github.com/trick77/lume/internal/imagegen` if needed by helper signatures.

In `availableTools`, after document tools, add:

```go
		for _, gen := range s.imageTools {
			schema := gen.Schema()
			if owner, exists := names[schema.Name]; exists {
				slog.Warn("skipping duplicate image tool name", "tool", schema.Name, "existing", owner)
				continue
			}
			names[schema.Name] = "built_in_image"
			tools = append(tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        schema.Name,
					Description: schema.Description,
					Parameters:  schema.Parameters,
				},
			})
		}
```

- [ ] **Step 5: Execute image tools**

In `executeBuiltInTool`, before document generation fallback, add:

```go
	if response, output, handled := s.executeImageTool(ctx, stream, user, thread, call); handled {
		return output, response, true
	}
```

Add helper functions:

```go
func (s *server) executeImageTool(ctx context.Context, stream *sse.Writer, user auth.User, thread chat.Thread, call llm.ToolCall) (*artifactResponse, string, bool) {
	generator := s.imageTool(call.Function.Name)
	if generator == nil {
		return nil, "", false
	}
	args, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		return nil, capToolOutput("tool failed: invalid arguments: "+err.Error()), true
	}
	req := imagegen.ToolRequest{}
	if prompt, _ := args["prompt"].(string); prompt != "" {
		req.Prompt = prompt
	}
	if filename, _ := args["filename"].(string); filename != "" {
		req.Filename = filename
	}
	if format, _ := args["output_format"].(string); format != "" {
		req.OutputFormat = format
	}
	if width, ok := numberArg(args["width"]); ok {
		req.Width = width
	}
	if height, ok := numberArg(args["height"]); ok {
		req.Height = height
	}
	if safety, ok := numberArg(args["safety_tolerance"]); ok {
		req.SafetyTolerance = &safety
	}
	if seed, ok := int64Arg(args["seed"]); ok {
		req.Seed = &seed
	}
	var buffer bytes.Buffer
	meta, err := generator.Generate(ctx, req, &buffer)
	if err != nil {
		return nil, capToolOutput("tool failed: "+err.Error()), true
	}
	if buffer.Len() > artifact.MaxArtifactSizeBytes {
		return nil, "tool failed: generated image is too large", true
	}
	out, file, err := artifact.CreateOutputFile(artifact.OutputRequest{
		UsersDir:        s.usersDir,
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: meta.DisplayFilename,
		Extension:       meta.Extension,
	})
	if err != nil {
		return nil, capToolOutput("tool failed: "+err.Error()), true
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: write artifact: "+err.Error()), true
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: close artifact: "+err.Error()), true
	}
	created, err := s.artifacts.Create(ctx, artifact.CreateInput{
		UserID:          user.ID,
		ThreadID:        thread.ID,
		ProjectID:       thread.ProjectID,
		DisplayFilename: out.DisplayFilename,
		VolumeRelPath:   out.VolumeRelPath,
		MIMEType:        meta.MIMEType,
		SizeBytes:       int64(buffer.Len()),
	})
	if err != nil {
		_ = os.Remove(out.AbsPath)
		return nil, capToolOutput("tool failed: persist artifact: "+err.Error()), true
	}
	response := artifactResponse{
		ID:              created.ID,
		DisplayFilename: created.DisplayFilename,
		MIMEType:        created.MIMEType,
		SizeBytes:       created.SizeBytes,
		ProjectID:       created.ProjectID,
		DownloadURL:     created.DownloadURL,
	}
	_ = sendSSEJSON(stream, "artifact", response)
	return &response, fmt.Sprintf("created image artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), true
}

func (s *server) imageTool(name string) *imagegen.Tool {
	for i := range s.imageTools {
		if s.imageTools[i].ToolName() == name {
			return &s.imageTools[i]
		}
	}
	return nil
}

func numberArg(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

func int64Arg(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}
```

Note: if `numberArg` duplicates an existing helper, reuse the existing helper and keep only one implementation.

- [ ] **Step 6: Run HTTP API tests**

Run: `cd backend && go test ./internal/httpapi`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/message_stream_handlers.go backend/internal/httpapi/message_stream_handlers_test.go
git commit -m "feat: expose image generation as a chat tool"
```

---

### Task 7: Wire BFL Client From Main

**Files:**
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Add imagegen import**

In `backend/cmd/slopr/main.go`, add:

```go
	"github.com/trick77/lume/internal/imagegen"
```

- [ ] **Step 2: Instantiate configured image tools**

After `docTools := []docgen.Generator{...}`, add:

```go
	var imageTools []imagegen.Tool
	if bflImageConfigured(cfg) {
		imageProvider := imagegen.NewBFLClient(imagegen.BFLConfig{
			BaseURL: cfg.BFLBaseURL,
			APIKey:  cfg.BFLAPIKey,
			Model:   cfg.BFLModel,
		})
		imageTools = append(imageTools, imagegen.NewTool(imageProvider))
	}
```

- [ ] **Step 3: Pass image tools to the HTTP API**

In the `httpapi.New(httpapi.Deps{...})` call, add:

```go
		ImageTools:            imageTools,
```

- [ ] **Step 4: Run backend tests**

Run: `cd backend && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/slopr/main.go
git commit -m "feat: wire BFL image generation provider"
```

---

### Task 8: Render Image Artifacts Inline

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/api.test.ts` or create a component test if the project has a better local pattern.

- [ ] **Step 1: Add a failing frontend test**

If `ChatShell` tests already render artifact cards, add an assertion that an artifact with `mimeType: "image/png"` renders an `<img>` with `src` equal to its `downloadUrl` and `alt` equal to `displayFilename`.

Expected JSX assertion:

```ts
expect(screen.getByRole("img", { name: "robot.png" })).toHaveAttribute("src", "/api/artifacts/art_1/download");
```

- [ ] **Step 2: Run frontend tests to verify failure**

Run: `make fe-test`

Expected: FAIL because `GeneratedArtifactCard` currently always renders the file card icon and never an image preview.

- [ ] **Step 3: Update `GeneratedArtifactCard`**

In `frontend/src/ChatShell.tsx`, replace `GeneratedArtifactCard` with:

```tsx
function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState("");
  const isImage = artifact.mimeType.startsWith("image/");

  async function handleDownload() {
    setError("");
    try {
      const blob = await downloadArtifact(artifact.downloadUrl);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = artifact.displayFilename;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch {
      setError("Download failed");
    }
  }

  return (
    <div className="max-w-[28rem] overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-[#f3f0e8]">
      {isImage && (
        <img
          className="block max-h-[28rem] w-full bg-[#1f1f1d] object-contain"
          src={artifact.downloadUrl}
          alt={artifact.displayFilename}
          loading="lazy"
        />
      )}
      <div className="flex items-center gap-3 px-4 py-3">
        {!isImage && (
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
            <FileIcon />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="slopr-message-text truncate">{artifact.displayFilename}</div>
          <div className="slopr-meta-text text-[#aaa79e]">
            {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
          </div>
          {error !== "" && <div className="slopr-meta-text text-[#d36f67]">{error}</div>}
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={handleDownload}
          type="button"
          title={`Download ${artifact.displayFilename}`}
          aria-label={`Download ${artifact.displayFilename}`}
        >
          <DownloadIcon />
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run frontend tests**

Run: `make fe-test`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/api.test.ts
git commit -m "feat: preview generated image artifacts"
```

---

### Task 9: Manual End-to-End Verification With Real BFL Key

**Files:**
- No source changes unless a verified defect is found.

- [ ] **Step 1: Run backend and frontend tests**

Run: `make test`

Expected: PASS.

Run: `make fe-test`

Expected: PASS.

- [ ] **Step 2: Build frontend and backend**

Run: `make fe-build`

Expected: PASS.

Run: `make build`

Expected: PASS and `bin/slopr` exists.

- [ ] **Step 3: Restore tracked web placeholders after local frontend build**

Run: `git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html`

Expected: no generated dist assets remain staged.

- [ ] **Step 4: Run Lume with image generation enabled**

Use a local DB and your real BFL key through the environment. Do not put the key in any file:

```bash
SLOPR_SESSION_SECRET=dev-secret \
SLOPR_AUTH_MODE=dev \
SLOPR_ADDR=127.0.0.1:18081 \
SLOPR_PUBLIC_URL=http://localhost:18081 \
SLOPR_DB_PATH=/private/tmp/slopr-imagegen-dev.db \
SLOPR_BFL_API_KEY="$BFL_API_KEY" \
SLOPR_BFL_MODEL=flux-2-klein-4b \
./bin/slopr
```

Expected startup summary line: `startup capability name="BFL image generation" status=enabled detail="model=flux-2-klein-4b tools=1"`.

- [ ] **Step 5: Generate an image in chat**

Open `http://localhost:18081`, sign in with dev auth, create a thread, and send:

```text
Generate an image of a warm editorial desk setup with a notebook, a small lamp, and a quiet chat interface on a laptop. Save it as editorial-desk.
```

Expected:

- SSE stream shows a `tool_call` for `generate_image`.
- SSE stream shows an `artifact` event with `mimeType` `image/png` or `image/jpeg`.
- The assistant message contains an inline image preview.
- Download button saves the generated image.
- The file is under `files/outputs/` for project-less chat or `projects/<project-id>/outputs/` for project chat.

- [ ] **Step 6: Check git status**

Run: `git status --short`

Expected: only intended source/doc changes are present; no real API keys and no built frontend assets are staged.

---

## Scope Deliberately Deferred

- No image editing in this first milestone.
- No reference-image upload flow.
- No ComfyUI/fal/Replicate provider.
- No separate image-only chat.
- No DB schema for image prompts/seeds/model metadata; this can be added later if replay/history search becomes important.
- No admin UI for provider settings; secrets remain environment-only.

---

## Self-Review

- **Spec coverage:** The plan implements BFL FLUX.2 [klein] text-to-image, built-in chat tool exposure, artifact persistence, inline frontend previews, config, and manual verification with a real key.
- **Security:** The BFL key stays in env only. Generated files use existing sandboxed artifact output paths. Artifact downloads remain scoped by user ID.
- **No placeholders:** Every implementation task has concrete files, code shape, commands, and expected results. The only flexible part is adapting the HTTP stream test to existing helper names, because the local helper file already defines fakes used across tests.
- **Type consistency:** `imagegen.Provider`, `imagegen.Tool`, `ToolRequest`, `ToolMeta`, `BFLConfig`, and `GenerateRequest` are defined before use in wiring tasks.
