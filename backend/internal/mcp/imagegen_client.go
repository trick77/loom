package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/imagegen"
)

// The image tool's contract (docs/imagegen.md) as loom addresses it, tied to
// the default server's own names so a rename there moves these too.
const (
	ImagegenServer         = imagegen.ServerName
	ImagegenTool           = imagegen.GenerateToolName
	ImagegenArgInputImages = imagegen.ArgInputImages
	ImagegenArgTextHeavy   = imagegen.ArgTextHeavy
	ImagegenArgModel       = imagegen.ArgModel
	// ImagegenMaxInputBytes is the largest input image the contract takes.
	ImagegenMaxInputBytes = imagegen.MaxInputImageBytes
	// ImagegenMaxPromptRunes is the longest prompt the contract takes.
	ImagegenMaxPromptRunes = imagegen.MaxPromptRunes
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
	// The same gate a server's image block passes.
	img, ok := checkImage(out.MIMEType, out.Bytes, artifact.MaxArtifactSizeBytes)
	if !ok {
		return CallResult{Text: "The image provider returned no usable image.", DroppedImages: 1}, nil
	}
	img.Filename, img.Model, img.Provider = baseName(out.Filename), out.Model, out.Provider
	img.Width, img.Height, img.DurationMs = out.Width, out.Height, out.DurationMs
	// No text: loom's "created image artifact" line is the whole result.
	return CallResult{Images: []Image{img}}, nil
}

func (c *imagegenClient) Close() error { return nil }
