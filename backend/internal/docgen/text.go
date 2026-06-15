package docgen

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

// allowedTextExtensions are the UTF-8 text formats create_text_file may emit.
// Kept in sync with the tool description and artifact.MIMEType so an extension
// inferred from the filename always maps to a sensible MIME type.
var allowedTextExtensions = map[string]bool{
	"txt": true, "md": true, "csv": true, "json": true,
	"html": true, "xml": true, "yaml": true, "yml": true, "log": true,
}

func isAllowedTextExtension(ext string) bool {
	return allowedTextExtensions[ext]
}

type TextGenerator struct {
	MaxInputBytes int
}

func (g TextGenerator) ToolName() string {
	return "create_text_file"
}

func (g TextGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a UTF-8 text-like file such as txt, md, csv, json, html, xml, yaml, or log." + FileToolGuardrail,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":  map[string]any{"type": "string", "description": "Short, descriptive output filename based on the file's content, without a path or file extension (e.g. `notes`)."},
				"extension": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "content"},
			"additionalProperties": false,
		},
	}
}

func (g TextGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	limit := g.MaxInputBytes
	if limit == 0 {
		limit = MaxGeneratedInputBytes
	}
	if len(content) > limit {
		return GeneratedMeta{}, fmt.Errorf("content is too large")
	}
	extension := "txt"
	if raw, ok := req.Payload["extension"].(string); ok && strings.TrimSpace(raw) != "" {
		extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	} else if fromName := strings.TrimPrefix(strings.ToLower(filepath.Ext(req.Filename)), "."); isAllowedTextExtension(fromName) {
		// No explicit extension given: honor a recognized extension already in the
		// filename (e.g. "report.md") instead of silently defaulting to txt, which
		// otherwise rewrites the name to .txt and mislabels the MIME type.
		extension = fromName
	}
	if _, err := io.WriteString(w, content); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{
		DisplayFilename: req.Filename,
		Extension:       extension,
		MIMEType:        artifact.MIMEType(extension),
	}, nil
}
