package docgen

import (
	"context"
	"io"
)

// MaxGeneratedInputBytes is the maximum size in bytes (1 MiB) of content that generators will process.
const MaxGeneratedInputBytes = 1 << 20

// FileToolGuardrail is appended to every file-creation tool description so the
// model only produces a downloadable file on an explicit request. Without it the
// model tends to save summaries and answers to files the user never asked for.
// The trigger must be the requested artifact (a file/document/format), never the
// verb alone: tool-eager models read "Create an elevator pitch" as "create a file".
// It is the short form: loom's system prompt carries the full rule once
// whenever these tools are offered, where the long form in all five
// descriptions cost ~2.8 KB per turn.
const FileToolGuardrail = " Call this ONLY when the user asks for a file, document or download, " +
	"or names a file format; writing content (a pitch, summary, plan, email) is an inline answer."

// GenerateRequest specifies the content and target format for a file generation request.
type GenerateRequest struct {
	Format   string
	Filename string
	Payload  map[string]any
	// Context is optional; generators that call out to a sidecar (PDF → Gotenberg)
	// use it to bound the render. Nil is treated as context.Background().
	Context context.Context
}

// GeneratedMeta contains metadata about a generated file.
type GeneratedMeta struct {
	DisplayFilename string
	Extension       string
	MIMEType        string
}

// Generator is the interface that all file format generators implement.
type Generator interface {
	ToolName() string
	Schema() ToolSchema
	Generate(GenerateRequest, io.Writer) (GeneratedMeta, error)
}

// ToolSchema defines the JSON schema and metadata for a file generation tool.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}
