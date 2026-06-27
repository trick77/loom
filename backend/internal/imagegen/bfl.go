package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBFLPollInterval = 750 * time.Millisecond
	// Keep in sync with config's runtime default.
	defaultBFLPollTimeout  = 1 * time.Minute
	maxDownloadedImageSize = 25 << 20
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
	model := c.effectiveModel(req.Model)
	submitted, err := c.submit(ctx, req, model)
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
		Model:       model,
		RequestID:   submitted.ID,
		Prompt:      req.Prompt,
		Seed:        req.Seed,
		Width:       req.Width,
		Height:      req.Height,
		CostCredits: submitted.Cost,
	}, nil
}

// effectiveModel returns the per-request model override when one is supplied
// (trimmed of surrounding slashes/space, matching NewBFLClient's normalization),
// otherwise the client's configured default.
func (c *BFLClient) effectiveModel(override string) string {
	if m := strings.Trim(strings.TrimSpace(override), "/"); m != "" {
		return m
	}
	return c.model
}

func (c *BFLClient) submit(ctx context.Context, req GenerateRequest, model string) (bflSubmitResponse, error) {
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
	// Forward source images for direct editing/transformation. BFL accepts a
	// raw base64-encoded image (no data: prefix) in input_image, with extra
	// references as input_image_2, input_image_3, … — so the model edits the
	// actual pixels instead of a lossy text re-description.
	for i, img := range req.InputImages {
		field := "input_image"
		if i > 0 {
			field = fmt.Sprintf("input_image_%d", i+1)
		}
		payload[field] = base64.StdEncoding.EncodeToString(img)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return bflSubmitResponse{}, err
	}
	endpoint := c.baseURL + "/" + url.PathEscape(model)
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
			if ctx.Err() != nil {
				return bflStatusResponse{}, bflPollContextError(ctx.Err())
			}
			return bflStatusResponse{}, err
		}
		switch strings.ToLower(status.Status) {
		case "ready", "completed", "succeeded":
			return status, nil
		case "pending", "processing", "queued":
			// not terminal: keep polling
		case "request moderated":
			return bflStatusResponse{}, fmt.Errorf("BFL blocked the prompt (request moderated); revise the prompt and try again")
		case "content moderated":
			return bflStatusResponse{}, fmt.Errorf("BFL blocked the generated image (content moderated); try a different prompt")
		case "error", "failed":
			return bflStatusResponse{}, fmt.Errorf("BFL generation failed (status: %s)", status.Status)
		default:
			return bflStatusResponse{}, fmt.Errorf("BFL returned an unexpected status: %s", status.Status)
		}
		select {
		case <-ctx.Done():
			return bflStatusResponse{}, bflPollContextError(ctx.Err())
		case <-ticker.C:
		}
	}
}

func bflPollContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("BFL generation canceled: %w", err)
	}
	return fmt.Errorf("BFL generation timed out: %w", err)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadedImageSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxDownloadedImageSize {
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
