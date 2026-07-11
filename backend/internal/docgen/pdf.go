package docgen

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/trick77/loom/internal/artifact"
)

// pdfRenderer is the subset of GotenbergClient that PDFGenerator needs, so tests
// can substitute a fake.
type pdfRenderer interface {
	Convert(ctx context.Context, html string, assets []gotenbergAsset, opts convertOptions) ([]byte, error)
}

// PDFGenerator renders a styled PDF by building HTML and converting it via a
// Gotenberg (Chromium) sidecar.
type PDFGenerator struct {
	client pdfRenderer
}

// NewPDFGenerator wires the Gotenberg client into the generator.
func NewPDFGenerator(client *GotenbergClient) PDFGenerator {
	return PDFGenerator{client: client}
}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func (g PDFGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	title, _ := req.Payload["title"].(string)
	blocks := parseBlocks(req.Payload)
	content, _ := req.Payload["content"].(string)
	if len(blocks) == 0 && strings.TrimSpace(content) == "" {
		// Distinguish "nothing provided" from "blocks were provided but parsed to
		// zero usable items" (e.g. a JSON-string that failed to decode, or items
		// missing a "type"). The specific message reaches both the model and the
		// dispatcher log so the actual payload shape is diagnosable.
		if raw, present := req.Payload["blocks"]; present {
			if arr, ok := raw.([]any); ok && len(arr) == 0 {
				return GeneratedMeta{}, errors.New("blocks array was empty; add blocks (each with a \"type\"), or supply markdown in \"content\"")
			}
			return GeneratedMeta{}, fmt.Errorf("blocks were provided but none were parseable (got %T); pass blocks as a JSON array of objects each with a \"type\", or supply markdown in \"content\"", raw)
		}
		return GeneratedMeta{}, errors.New("content or blocks are required")
	}
	subtitle, _ := req.Payload["subtitle"].(string)
	if strings.TrimSpace(title) == "" {
		title = "Document"
	}

	// Prefer the structured blocks path; otherwise render the markdown content
	// fallback through a real markdown renderer (renderMarkdownBody).
	var html string
	if len(blocks) > 0 {
		html = renderHTML(title, subtitle, blocks)
	} else {
		html = renderHTMLWithBody(title, subtitle, renderMarkdownBody(content))
	}
	if g.client == nil {
		return GeneratedMeta{}, errors.New("pdf renderer is not configured")
	}
	pdf, err := g.client.Convert(req.Context, html, fontAssets(), defaultConvertOptions())
	if err != nil {
		return GeneratedMeta{}, err
	}
	if _, err := w.Write(pdf); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pdf", MIMEType: artifact.MIMEType("pdf")}, nil
}

func (g PDFGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create a styled PDF report. Prefer the structured 'blocks' array (heading, " +
			"paragraph, bullets, table, columns, callout, code) over a flat text string: use headings to " +
			"structure sections, tables for tabular data, callouts to emphasize key points, and code " +
			"blocks (rendered monospaced) for code samples or terminal output — put the code in 'text' and " +
			"optionally set 'language' (e.g. \"go\", \"python\") for syntax highlighting. " +
			"Use ✓ or ✗ anywhere in text to show a green-check / red-cross status marker " +
			"(handy for feature-comparison tables). " +
			"'content' is accepted as a simple Markdown fallback." + FileToolGuardrail,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string", "description": "Short, descriptive output filename based on the document's content, without a path or file extension (e.g. `quarterly-report`)."},
				"title":    map[string]any{"type": "string"},
				"subtitle": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string", "description": "Markdown fallback when blocks is omitted."},
				"blocks": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":     map[string]any{"type": "string", "enum": []string{"heading", "paragraph", "bullets", "table", "columns", "callout", "code"}},
							"level":    map[string]any{"type": "integer", "description": "heading block: depth 1 = section heading, 2 = subheading. Values above 2 render as a subheading."},
							"text":     map[string]any{"type": "string", "description": "The text for heading, paragraph, callout, and code blocks (put source code here for a code block)."},
							"language": map[string]any{"type": "string", "description": "Optional syntax-highlighting language for a code block (e.g. `go`, `python`, `json`)."},
							"items":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "bullets block: the list items."},
							"rows":     map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "description": "table block: rows of cells; the first row is the header."},
							"left":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "columns block: left-column lines."},
							"right":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "columns block: right-column lines."},
						},
						"required": []string{"type"},
					},
				},
			},
			"required":             []string{"filename"},
			"additionalProperties": false,
		},
	}
}
