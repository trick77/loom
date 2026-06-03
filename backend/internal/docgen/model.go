package docgen

import "io"

const MaxGeneratedInputBytes = 1 << 20

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
