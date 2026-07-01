package docgen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultGotenbergTimeout = 60 * time.Second

// maxPDFBytes caps how much of a Gotenberg response we read, so a runaway render
// can't exhaust memory. The tool dispatcher enforces its own artifact-size limit.
const maxPDFBytes = 32 << 20 // 32 MiB

// GotenbergConfig configures a GotenbergClient. HTTPClient is optional.
type GotenbergConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// GotenbergClient renders HTML to PDF via a Gotenberg (Chromium) sidecar.
type GotenbergClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewGotenbergClient builds a GotenbergClient, applying a default HTTP client
// with a generous timeout when none is supplied.
func NewGotenbergClient(cfg GotenbergConfig) *GotenbergClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultGotenbergTimeout}
	}
	return &GotenbergClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		httpClient: client,
	}
}

// gotenbergAsset is one extra multipart file (e.g. a font) referenced by
// index.html via a relative path.
type gotenbergAsset struct {
	Filename string
	Data     []byte
}

// convertOptions controls Chromium's page setup. Zero values fall back to the
// A4 defaults applied in Convert.
type convertOptions struct {
	PaperWidth      float64 // inches
	PaperHeight     float64 // inches
	MarginTop       float64 // inches
	MarginBottom    float64
	MarginLeft      float64
	MarginRight     float64
	PrintBackground bool
}

func defaultConvertOptions() convertOptions {
	return convertOptions{
		PaperWidth: 8.27, PaperHeight: 11.69, // A4
		MarginTop: 0.5, MarginBottom: 0.5, MarginLeft: 0.5, MarginRight: 0.5,
		PrintBackground: true,
	}
}

// Convert POSTs index.html (plus optional assets) to Gotenberg's Chromium
// HTML→PDF endpoint and returns the PDF bytes. Error messages are written to be
// legible: they surface to the model as the tool-failure reason.
func (c *GotenbergClient) Convert(ctx context.Context, html string, assets []gotenbergAsset, opts convertOptions) ([]byte, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultGotenbergTimeout)
		defer cancel()
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// index.html is the required main document; assets are referenced by name.
	if err := writeFilePart(mw, "index.html", []byte(html)); err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	for _, a := range assets {
		if err := writeFilePart(mw, a.Filename, a.Data); err != nil {
			return nil, fmt.Errorf("build gotenberg request: %w", err)
		}
	}

	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	fields := map[string]string{
		"paperWidth":      num(opts.PaperWidth),
		"paperHeight":     num(opts.PaperHeight),
		"marginTop":       num(opts.MarginTop),
		"marginBottom":    num(opts.MarginBottom),
		"marginLeft":      num(opts.MarginLeft),
		"marginRight":     num(opts.MarginRight),
		"printBackground": strconv.FormatBool(opts.PrintBackground),
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("build gotenberg request: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		pdf, err := io.ReadAll(io.LimitReader(resp.Body, maxPDFBytes))
		if err != nil {
			return nil, fmt.Errorf("read gotenberg response: %w", err)
		}
		return pdf, nil
	case resp.StatusCode == http.StatusBadRequest:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("gotenberg rejected the document (400): %s", strings.TrimSpace(string(msg)))
	case resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusServiceUnavailable:
		return nil, fmt.Errorf("gotenberg unavailable (%d)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("gotenberg conversion failed: status %d", resp.StatusCode)
	}
}

// writeFilePart adds a "files" multipart part with the given filename and bytes.
func writeFilePart(mw *multipart.Writer, filename string, data []byte) error {
	w, err := mw.CreateFormFile("files", filename)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
