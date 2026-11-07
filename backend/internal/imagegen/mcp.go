package imagegen

import (
	"context"
	"encoding/json"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

const (
	// toolTimeout bounds one generation: the provider's queue and render,
	// inside the client's 3 min call limit (loom's mcp.ArtifactCallTimeout).
	toolTimeout = 170 * time.Second
	// maxArgBytes fits arguments carrying one largest input image as base64:
	// loom sends one (the edit source), and every byte here is read whole.
	maxArgBytes = MaxInputImageBytes/3*4 + 1<<20
	// maxImageBytes is one artifact's size (loom's artifact.MaxArtifactSizeBytes):
	// the most a client that makes artifacts takes in one reply.
	maxImageBytes = 25 << 20
)

// MCPServer is the image tool as an MCP server: the contract loom addresses
// (docs/imagegen.md). loom-imagegen serves it; loom's tests serve it too, so
// what they drive is what ships.
func MCPServer(svc *Service, version string) mcpserver.Server {
	return mcpserver.Server{
		Name:          ServerName,
		Version:       version,
		ToolTimeout:   toolTimeout,
		MaxImageBytes: maxImageBytes,
		Tools: []mcpserver.Tool{{
			Name:        GenerateToolName,
			Title:       "Generate an image",
			Description: svc.Description(),
			Schema:      svc.Schema(),
			OpenWorld:   true,
			MaxArgBytes: maxArgBytes,
			Run: func(ctx context.Context, raw json.RawMessage) mcpserver.Result {
				out, err := svc.Call(ctx, raw)
				if err != nil {
					return mcpserver.ErrorResult(err.Error())
				}
				// No text: loom names the artifact it made.
				return mcpserver.Result{Images: []mcpserver.Image{{
					MimeType: out.MIMEType,
					Data:     out.Bytes,
					Meta: map[string]any{
						"filename": out.Filename, "model": out.Model, "provider": out.Provider,
						"width": out.Width, "height": out.Height, "duration_ms": out.DurationMs,
					},
				}}}
			},
		}},
	}
}
