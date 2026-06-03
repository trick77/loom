package docgen

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/trick77/spark/internal/artifact"
)

type TextGenerator struct {
	MaxInputBytes int
}

func (g TextGenerator) ToolName() string {
	return "create_text_file"
}

func (g TextGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a UTF-8 text-like file such as txt, md, csv, json, html, xml, yaml, or log.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":  map[string]any{"type": "string"},
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
