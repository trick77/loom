package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/imagegen"
)

// imagegenClient serves the image tool in-process, through the same MCP
// contract (docs/imagegen.md) an mcp.json server named imagegen answers: the
// image comes back as an Image, as it would from an image content block.
type imagegenClient struct {
	serverName string
	svc        *imagegen.Service
}

// ImagegenServerConfig is the built-in image server: in-process, allowed to
// make artifacts.
func ImagegenServerConfig(svc *imagegen.Service) ServerConfig {
	return ServerConfig{
		Transport: TransportInProcess,
		Artifacts: true,
		InProcess: func(name string) Client { return &imagegenClient{serverName: name, svc: svc} },
	}
}

func (c *imagegenClient) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{
		Name:         ExposedToolName(c.serverName, imagegen.GenerateToolName),
		OriginalName: imagegen.GenerateToolName,
		Title:        "Generate an image",
		Description:  c.svc.Description(),
		InputSchema:  c.svc.Schema(),
		ServerName:   c.serverName,
	}}, nil
}

func (c *imagegenClient) CallTool(ctx context.Context, name string, arguments map[string]any) (CallResult, error) {
	if name != imagegen.GenerateToolName {
		return CallResult{}, fmt.Errorf("unknown imagegen tool %q", name)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return CallResult{}, err
	}
	out, err := c.svc.Call(ctx, raw)
	if err != nil {
		return CallResult{}, err
	}
	// The same checks a server's image block passes: a canonical raster type
	// whose bytes are that type, within the artifact size.
	mime, ext, ok := artifact.CanonicalImageMIME(out.MIMEType)
	if !ok || len(out.Bytes) == 0 || len(out.Bytes) > artifact.MaxArtifactSizeBytes || http.DetectContentType(out.Bytes) != mime {
		return CallResult{Text: "The image provider returned no usable image.", DroppedImages: 1}, nil
	}
	// No text: loom's "created image artifact" line is the whole result.
	return CallResult{
		Images: []Image{{
			Filename: baseName(out.Filename), MimeType: mime, Extension: ext, Data: out.Bytes,
			Model: out.Model, Provider: out.Provider, Width: out.Width, Height: out.Height, DurationMs: out.DurationMs,
		}},
	}, nil
}

func (c *imagegenClient) Close() error { return nil }
