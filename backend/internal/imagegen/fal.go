package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/loom/internal/inference"
)

const (
	defaultFalPollInterval = 750 * time.Millisecond
	// Keep in sync with config's runtime default.
	defaultFalPollTimeout  = 1 * time.Minute
	maxDownloadedImageSize = 25 << 20
)

type FalConfig struct {
	BaseURL      string
	APIKey       string
	Model        string
	PollInterval time.Duration
	PollTimeout  time.Duration
	HTTPClient   *http.Client
}

type FalClient struct {
	baseURL      string
	apiKey       string
	model        string
	pollInterval time.Duration
	pollTimeout  time.Duration
	httpClient   *http.Client
}

func NewFalClient(cfg FalConfig) *FalClient {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultFalPollInterval
	}
	pollTimeout := cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultFalPollTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &FalClient{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
		model:        strings.Trim(strings.TrimSpace(cfg.Model), "/"),
		pollInterval: pollInterval,
		pollTimeout:  pollTimeout,
		httpClient:   client,
	}
}

// Generate submits a generation to fal's queue, polls it to completion, fetches
// the result document and downloads the image. It emits one inference log line
// for the whole round trip (not one per poll), on the success and on every
// failure path, so image generation is accounted for in the same log stream as
// the chat and embedding calls. The prompt is never logged — only the request
// shape and the resulting size.
func (c *FalClient) Generate(ctx context.Context, input GenerateRequest) (GenerateResult, error) {
	ctx = inference.WithDefaultPurpose(ctx, "image_generate")
	start := time.Now()
	req, err := input.Normalized()
	if err != nil {
		inference.LogFailed(ctx, c.effectiveModel(input.Model), time.Since(start), err)
		return GenerateResult{}, err
	}
	model := c.effectiveModel(req.Model)
	req.Width, req.Height = clampToFalBounds(req.Width, req.Height)
	shape := []slog.Attr{
		slog.Int("width", req.Width),
		slog.Int("height", req.Height),
		slog.Int("input_images", len(req.InputImages)),
	}
	fail := func(err error) (GenerateResult, error) {
		inference.LogFailed(ctx, model, time.Since(start), err, shape...)
		return GenerateResult{}, err
	}
	submitted, err := c.submit(ctx, req, model)
	if err != nil {
		return fail(err)
	}
	status, err := c.poll(ctx, submitted.StatusURL)
	if err != nil {
		return fail(err)
	}
	responseURL := strings.TrimSpace(status.ResponseURL)
	if responseURL == "" {
		responseURL = strings.TrimSpace(submitted.ResponseURL)
	}
	if responseURL == "" {
		return fail(fmt.Errorf("fal result did not include a response URL"))
	}
	result, err := c.fetchResult(ctx, responseURL)
	if err != nil {
		return fail(err)
	}
	if len(result.HasNSFWConcepts) > 0 && result.HasNSFWConcepts[0] {
		return fail(fmt.Errorf("fal blocked the generated image (content moderated); try a different prompt"))
	}
	if len(result.Images) == 0 || strings.TrimSpace(result.Images[0].URL) == "" {
		return fail(fmt.Errorf("fal result did not include an image URL"))
	}
	body, contentType, err := c.download(ctx, strings.TrimSpace(result.Images[0].URL))
	if err != nil {
		return fail(err)
	}
	done := append(shape,
		slog.String("request_id", submitted.RequestID),
		slog.Int("image_bytes", len(body)),
	)
	inference.LogCompleted(ctx, model, time.Since(start), done...)
	mimeType := MIMEType(req.OutputFormat)
	if ct := strings.TrimSpace(result.Images[0].ContentType); strings.HasPrefix(ct, "image/") {
		mimeType = strings.Split(ct, ";")[0]
	}
	if strings.HasPrefix(contentType, "image/") {
		mimeType = strings.Split(contentType, ";")[0]
	}
	return GenerateResult{
		Filename:  req.Filename,
		Extension: extensionForMIME(mimeType, req.OutputFormat),
		MIMEType:  mimeType,
		Bytes:     body,
		Provider:  "fal",
		Model:     model,
		RequestID: submitted.RequestID,
		Prompt:    req.Prompt,
		Seed:      req.Seed,
		Width:     req.Width,
		Height:    req.Height,
		// fal does not report a per-request price in the API response, so cost
		// stays unset (BFL returned one on submit).
		CostCredits: nil,
	}, nil
}

// effectiveModel returns the per-request model override when one is supplied
// (trimmed of surrounding slashes/space, matching NewFalClient's normalization),
// otherwise the client's configured default.
func (c *FalClient) effectiveModel(override string) string {
	if m := strings.Trim(strings.TrimSpace(override), "/"); m != "" {
		return m
	}
	return c.model
}

const (
	// falMinSide and falMaxSide are the per-side bounds every FLUX.2 endpoint on
	// fal declares for image_size (min_width/max_width in its OpenAPI schema).
	// The shared request validation is looser — it allows down to 64 px and caps
	// only the area — so dimensions are fitted here rather than sent through and
	// bounced as a 422 the user would see as a raw tool error. fal's max_area of
	// 4,194,304 is above MaxOutputPixels, so the area is already covered.
	falMinSide = 256
	falMaxSide = 2560

	// falMaxSafetyTolerance is the most permissive value fal's "1".."5" tolerance
	// enum accepts. Every request goes out at it; see submit for why.
	falMaxSafetyTolerance = "5"
)

// clampToFalBounds fits (w, h) into fal's per-side envelope, scaling down
// proportionally first (so an oversized request keeps its aspect ratio) and only
// then raising any side still below the floor. That order matters: clamping the
// long side first means the floor bump can never push the area back over the cap.
func clampToFalBounds(w, h int) (int, int) {
	w, h = ClampMaxSide(w, h, falMaxSide)
	if w < falMinSide {
		w = falMinSide
	}
	if h < falMinSide {
		h = falMinSide
	}
	return align16(w), align16(h)
}

// endpoint builds the queue URL for a model. fal model ids are multi-segment
// paths ("fal-ai/flux-2-pro"), so the id is joined into the path as-is rather
// than percent-escaped. Text-to-image and image editing are separate endpoints
// on fal, so a request carrying source images is routed to the "/edit" variant.
func (c *FalClient) endpoint(model string, editing bool) string {
	path := strings.Trim(model, "/")
	if editing {
		path += "/edit"
	}
	return c.baseURL + "/" + path
}

func (c *FalClient) submit(ctx context.Context, req GenerateRequest, model string) (falSubmitResponse, error) {
	payload := map[string]any{
		"prompt": req.Prompt,
		"image_size": map[string]int{
			"width":  req.Width,
			"height": req.Height,
		},
		"output_format": req.OutputFormat,
		// Moderation is pinned as far open as the provider allows, deliberately.
		// fal takes the tolerance as a string enum over 1-5 (1 strictest), so
		// falMaxSafetyTolerance is the most permissive value the endpoint accepts;
		// the caller's safety_tolerance is ignored rather than allowed to tighten
		// it back up, which is why it is not read here. enable_safety_checker is
		// asked to be off: fal gates that on an account authorization, and an
		// unauthorized request is checked anyway — a flagged image then comes back
		// as a black frame, which the has_nsfw_concepts check in Generate turns
		// into a real error instead of a blank picture.
		"safety_tolerance":      falMaxSafetyTolerance,
		"enable_safety_checker": false,
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}
	// Forward source images for direct editing/transformation. fal takes them as
	// a list of URLs and accepts base64 data URIs inline, so the model edits the
	// actual pixels instead of a lossy text re-description.
	if len(req.InputImages) > 0 {
		urls := make([]string, 0, len(req.InputImages))
		for _, img := range req.InputImages {
			urls = append(urls, dataURI(img))
		}
		payload["image_urls"] = urls
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return falSubmitResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(model, len(req.InputImages) > 0), bytes.NewReader(body))
	if err != nil {
		return falSubmitResponse{}, err
	}
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("authorization", "Key "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return falSubmitResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return falSubmitResponse{}, falHTTPError("submit", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out falSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return falSubmitResponse{}, err
	}
	if out.RequestID == "" || out.StatusURL == "" {
		return falSubmitResponse{}, fmt.Errorf("fal submit response missing request_id or status_url")
	}
	return out, nil
}

// dataURI encodes raw image bytes as a base64 data URI, sniffing the content
// type from the bytes themselves (fal rejects a mismatched declared type).
func dataURI(img []byte) string {
	mimeType := http.DetectContentType(img)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png"
	}
	return "data:" + strings.Split(mimeType, ";")[0] + ";base64," + base64.StdEncoding.EncodeToString(img)
}

func (c *FalClient) poll(ctx context.Context, statusURL string) (falStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.pollTimeout)
	defer cancel()
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		status, err := c.fetchStatus(ctx, statusURL)
		if err != nil {
			if ctx.Err() != nil {
				return falStatusResponse{}, falPollContextError(ctx.Err())
			}
			return falStatusResponse{}, err
		}
		if err := falStatusError(status); err != nil {
			return falStatusResponse{}, err
		}
		switch strings.ToUpper(strings.TrimSpace(status.Status)) {
		case "COMPLETED", "OK":
			return status, nil
		case "IN_QUEUE", "IN_PROGRESS":
			// not terminal: keep polling
		default:
			return falStatusResponse{}, fmt.Errorf("fal returned an unexpected status: %s", status.Status)
		}
		select {
		case <-ctx.Done():
			return falStatusResponse{}, falPollContextError(ctx.Err())
		case <-ticker.C:
		}
	}
}

// isFalContentPolicy reports whether a fragment of a fal error — a status
// document's error_type/detail, or a raw HTTP error body — describes a
// moderation refusal. fal has no dedicated "moderated" status the way BFL did,
// so the refusal is recognised by name wherever it surfaces.
func isFalContentPolicy(text string) bool {
	lowered := strings.ToLower(text)
	return strings.Contains(lowered, "content_policy") ||
		strings.Contains(lowered, "content policy") ||
		strings.Contains(lowered, "moderat")
}

// falContentPolicyError is the single phrasing for a moderation refusal, shared
// by every path that can carry one.
func falContentPolicyError() error {
	return fmt.Errorf("fal blocked the prompt (content policy); revise the prompt and try again")
}

// falHTTPError turns a non-2xx response from one of fal's queue endpoints into
// a user-facing message. A moderation refusal can arrive here rather than on the
// status document — as a 422 whose body carries a content_policy_violation type
// — and would otherwise reach the user as raw JSON. Everything else keeps the
// status and body, which is the useful detail for a real failure.
func falHTTPError(stage string, statusCode int, body string) error {
	if isFalContentPolicyBody(body) {
		return falContentPolicyError()
	}
	return fmt.Errorf("fal %s failed: status %d: %s", stage, statusCode, body)
}

// isFalContentPolicyBody recognises a refusal in an HTTP error body. fal echoes
// the whole rejected input back in these bodies, prompt included, so matching the
// raw text would report a plain validation failure as a content block whenever
// the user's own prompt happened to contain one of the words. The structured
// detail entries are matched instead, and the raw text only when the body is not
// in that shape.
func isFalContentPolicyBody(body string) bool {
	var parsed struct {
		Detail []struct {
			Type    string `json:"type"`
			Message string `json:"msg"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || len(parsed.Detail) == 0 {
		return isFalContentPolicy(body)
	}
	for _, entry := range parsed.Detail {
		if isFalContentPolicy(entry.Type) || isFalContentPolicy(entry.Message) {
			return true
		}
	}
	return false
}

// falStatusError turns an error reported on the queue status document into a
// user-facing message.
func falStatusError(status falStatusResponse) error {
	errType := strings.TrimSpace(status.ErrorType)
	detail := falErrorDetail(status.Error)
	if errType == "" && detail == "" {
		return nil
	}
	if isFalContentPolicy(errType) || isFalContentPolicy(detail) {
		return falContentPolicyError()
	}
	if detail == "" {
		detail = errType
	}
	return fmt.Errorf("fal generation failed: %s", detail)
}

func falPollContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("fal generation canceled: %w", err)
	}
	return fmt.Errorf("fal generation timed out: %w", err)
}

func (c *FalClient) fetchStatus(ctx context.Context, statusURL string) (falStatusResponse, error) {
	body, err := c.getJSON(ctx, statusURL, "poll")
	if err != nil {
		return falStatusResponse{}, err
	}
	var out falStatusResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return falStatusResponse{}, err
	}
	return out, nil
}

func (c *FalClient) fetchResult(ctx context.Context, responseURL string) (falResultResponse, error) {
	body, err := c.getJSON(ctx, responseURL, "result")
	if err != nil {
		return falResultResponse{}, err
	}
	var out falResultResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return falResultResponse{}, err
	}
	return out, nil
}

// getJSON performs an authenticated GET against one of fal's queue URLs. The
// URLs come from the submit/status documents, never built here.
func (c *FalClient) getJSON(ctx context.Context, endpoint, stage string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("authorization", "Key "+c.apiKey)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, falHTTPError(stage, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (c *FalClient) download(ctx context.Context, imageURL string) ([]byte, string, error) {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadedImageSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxDownloadedImageSize {
		return nil, "", fmt.Errorf("generated image is too large")
	}
	return body, resp.Header.Get("content-type"), nil
}

type falSubmitResponse struct {
	RequestID   string `json:"request_id"`
	StatusURL   string `json:"status_url"`
	ResponseURL string `json:"response_url"`
}

type falStatusResponse struct {
	Status      string `json:"status"`
	ResponseURL string `json:"response_url"`
	// Error is a plain string on some fal endpoints and an object on others, so
	// it is kept raw and rendered by falErrorDetail.
	Error     json.RawMessage `json:"error"`
	ErrorType string          `json:"error_type"`
}

// falErrorDetail renders the queue document's `error` field as a single line,
// unwrapping the JSON string form and falling back to the raw JSON otherwise.
func falErrorDetail(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return trimmed
}

type falResultResponse struct {
	Images []struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	} `json:"images"`
	HasNSFWConcepts []bool `json:"has_nsfw_concepts"`
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
