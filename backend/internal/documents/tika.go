package documents

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxExtractedTextBytes caps how much extracted text we keep per document, so a
// single huge upload cannot blow up memory or the embedding budget.
const maxExtractedTextBytes = 5 << 20 // 5 MiB of text

const defaultTikaTimeout = 2 * time.Minute

// TikaConfig configures a TikaClient. HTTPClient is optional (defaults applied).
type TikaConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// TikaClient extracts plain text from documents via an Apache Tika server.
type TikaClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewTikaClient builds a TikaClient, applying a default HTTP client with a
// generous timeout when none is supplied.
func NewTikaClient(cfg TikaConfig) *TikaClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTikaTimeout}
	}
	return &TikaClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		httpClient: client,
	}
}

// Ping verifies the Tika server is reachable via its GET /version endpoint. Used
// as a startup readiness probe so the backend can refuse to boot when document
// text extraction would be dead on arrival.
func (c *TikaClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/version", nil)
	if err != nil {
		return fmt.Errorf("build tika health request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tika health request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tika health check: status %d", resp.StatusCode)
	}
	return nil
}

// Extract streams the file bytes to Tika's `PUT /tika` endpoint and returns the
// extracted plain text, capped at maxExtractedTextBytes. The Content-Type hint
// helps Tika pick the right parser; Accept: text/plain selects plain output.
func (c *TikaClient) Extract(ctx context.Context, filename, mime string, r io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/tika", r)
	if err != nil {
		return "", fmt.Errorf("build tika request: %w", err)
	}
	req.Header.Set("Accept", "text/plain; charset=UTF-8")
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	if filename != "" {
		// Tika uses the resource name as an extraction hint.
		req.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tika request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tika extraction failed: status %d", resp.StatusCode)
	}

	// Read one byte past the cap so we can tell whether truncation occurred,
	// then trim back to the cap.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExtractedTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("read tika response: %w", err)
	}
	if len(body) > maxExtractedTextBytes {
		body = body[:maxExtractedTextBytes]
	}
	return string(body), nil
}
