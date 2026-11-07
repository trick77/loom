package docgen

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/trick77/toolkit/mcpserver"
)

// ServerName is the document tools' MCP server (docs/docgen.md). loom keeps
// its own copy (mcp.DocgenServer); loom-docgen's tests hold the two together.
const ServerName = "docgen"

const (
	// toolTimeout bounds one call: a PDF waits on Gotenberg for up to its
	// 60 s, inside the client's 3 min call limit (loom's mcp.ArtifactCallTimeout).
	toolTimeout = 90 * time.Second
	// maxArgBytes fits a whole document as the argument: the 1 MiB content
	// cap even when JSON escapes every byte (6 bytes for <), plus the
	// other fields. Every byte here is read whole.
	maxArgBytes = 6*MaxGeneratedInputBytes + 1<<20
	// MaxFileBytes is one artifact's size (loom's artifact.MaxArtifactSizeBytes):
	// the most a client that makes artifacts keeps of one reply.
	MaxFileBytes = 25 << 20
)

// MCPServer serves the generators as MCP tools under their own names and
// schemas, each returning its file as a binary resource and no text: loom
// names the artifact it made. loom-docgen serves it; loom's tests serve it
// too, so what they drive is what ships.
func MCPServer(generators []Generator, version string) mcpserver.Server {
	tools := make([]mcpserver.Tool, 0, len(generators))
	for _, gen := range generators {
		schema := gen.Schema()
		tools = append(tools, mcpserver.Tool{
			Name:        schema.Name,
			Description: schema.Description,
			Schema:      schema.Parameters,
			MaxArgBytes: maxArgBytes,
			Run: func(ctx context.Context, raw json.RawMessage) mcpserver.Result {
				return generate(ctx, gen, raw)
			},
		})
	}
	return mcpserver.Server{
		Name:         ServerName,
		Version:      version,
		ToolTimeout:  toolTimeout,
		MaxBlobBytes: MaxFileBytes,
		Tools:        tools,
	}
}

// generate is one call of a generator tool: its file, or an error the model
// reads.
func generate(ctx context.Context, gen Generator, raw json.RawMessage) mcpserver.Result {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return mcpserver.ErrorResult("invalid arguments: " + err.Error())
	}
	if args == nil {
		args = map[string]any{}
	}
	filename, _ := args["filename"].(string)
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{Format: gen.ToolName(), Filename: filename, Payload: args, Context: ctx}, &out)
	switch {
	case err != nil:
		return mcpserver.ErrorResult(err.Error())
	case out.Len() == 0:
		return mcpserver.ErrorResult("the generated file is empty")
	case out.Len() > MaxFileBytes:
		return mcpserver.ErrorResult("the generated file is too large")
	}
	return mcpserver.Result{Files: []mcpserver.File{{
		Name:     fileName(meta.DisplayFilename, meta.Extension),
		MimeType: meta.MIMEType,
		Data:     out.Bytes(),
	}}}
}

// fileName is the plain name a file goes out under: the model's name without
// any directory, ending in the extension generated (the client reads the type
// from it). A generator extension already there is replaced ("notes.txt" as
// md is "notes.md"); any other dot is part of the name ("Q3 v1.2 summary").
// A name with nothing left is "artifact".
func fileName(display, ext string) string {
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(strings.TrimSpace(display), `\`, "/")))
	if name == "." || name == "/" || name == ".." {
		name = ""
	}
	if have := path.Ext(name); !strings.EqualFold(have, "."+ext) {
		if slices.Contains(Extensions(), strings.ToLower(strings.TrimPrefix(have, "."))) {
			name = strings.TrimSuffix(name, have)
		}
		name += "." + ext
	}
	if strings.TrimSuffix(name, "."+ext) == "" {
		name = "artifact." + ext
	}
	return name
}
