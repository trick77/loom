package turntest

import (
	"context"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/mcp"
)

// ImageProvider is an image provider that returns a tiny fake PNG.
type ImageProvider struct{}

// Generate implements imagegen.Provider.
func (ImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	model := req.Model
	if model == "" {
		model = "fake-model"
	}
	return imagegen.GenerateResult{
		Filename:  req.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     model,
		RequestID: "request-1",
		Prompt:    req.Prompt,
		Width:     req.Width,
		Height:    req.Height,
	}, nil
}

// ImageService is the image tool served in-process behind its MCP contract,
// as loom wires the built-in: provider renders, defaultModel and
// typographyModel are the service's models ("" for none).
func ImageService(provider imagegen.Provider, defaultModel, typographyModel string) *mcp.Service {
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{Servers: map[string]mcp.ServerConfig{
		imagegen.ServerName: mcp.ImagegenServerConfig(imagegen.NewService(provider, defaultModel, typographyModel)),
	}}, mcp.Config{}, nil, nil)
	if err != nil {
		panic(err) // in-process discovery cannot fail
	}
	return svc
}
