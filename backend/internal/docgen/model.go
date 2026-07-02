package docgen

import (
	"context"
	"io"
)

const MaxGeneratedInputBytes = 1 << 20

// FileToolGuardrail is appended to every file-creation tool description so the
// model only produces a downloadable file on an explicit request. Without it the
// model tends to save summaries and answers to files the user never asked for.
// The trigger must be the requested artifact (a file/document/format), never the
// verb alone: tool-eager models read "Create an elevator pitch" as "create a file".
const FileToolGuardrail = " Call this ONLY when the user asks for a file, document, " +
	"or download, or names a file format (txt, md, csv, PDF, ...). A request to " +
	"create or write content — a pitch, summary, plan, email, answer — is an inline " +
	"response, NOT a file, unless a file or format is explicitly mentioned. For " +
	"summarize, explain, analyze, or answer requests — including about attached " +
	"documents — respond inline and do not create a file."

type GenerateRequest struct {
	Format   string
	Filename string
	Payload  map[string]any
	// Context is optional; generators that call out to a sidecar (PDF → Gotenberg)
	// use it to bound the render. Nil is treated as context.Background().
	Context context.Context
}

type GeneratedMeta struct {
	DisplayFilename string
	Extension       string
	MIMEType        string
}

type Generator interface {
	ToolName() string
	Schema() ToolSchema
	Generate(GenerateRequest, io.Writer) (GeneratedMeta, error)
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}
