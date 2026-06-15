package docgen

import "io"

const MaxGeneratedInputBytes = 1 << 20

// FileToolGuardrail is appended to every file-creation tool description so the
// model only produces a downloadable file on an explicit request. Without it the
// model tends to save summaries and answers to files the user never asked for.
const FileToolGuardrail = " Call this ONLY when the user explicitly asks to save, " +
	"create, export, or download a file. For summarize, explain, analyze, or answer " +
	"requests — including about attached documents — respond inline instead and do not " +
	"create a file."

type GenerateRequest struct {
	Format   string
	Filename string
	Payload  map[string]any
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
