package turntest

import (
	"context"

	"github.com/trick77/loom/internal/imagegen"
)

// ImageProvider is an image provider that returns a tiny fake PNG.
type ImageProvider struct{}

// Generate implements imagegen.Provider.
func (ImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	return imagegen.GenerateResult{
		Filename:  req.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    req.Prompt,
		Width:     req.Width,
		Height:    req.Height,
	}, nil
}
