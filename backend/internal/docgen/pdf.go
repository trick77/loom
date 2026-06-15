package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
	"github.com/johnfercher/maroto/v2/pkg/repository"
	"github.com/trick77/lume/internal/artifact"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

const pdfFontFamily = "lume"

type PDFGenerator struct{}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func rgbColor(c RGB) *props.Color { return &props.Color{Red: c.R, Green: c.G, Blue: c.B} }

// newMaroto builds a maroto instance with the Lume fonts and an accent header band.
func newMaroto(title, subtitle string) (core.Maroto, error) {
	fonts, err := repository.New().
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Normal, goregular.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Bold, gobold.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Italic, goitalic.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.BoldItalic, gobolditalic.TTF).
		Load()
	if err != nil {
		return nil, err
	}
	cfg := config.NewBuilder().
		WithCustomFonts(fonts).
		WithDefaultFont(&props.Font{Family: pdfFontFamily}).
		Build()
	m := maroto.New(cfg)

	band := col.New(12).WithStyle(&props.Cell{BackgroundColor: rgbColor(Theme.Accent)})
	band.Add(text.New(title, props.Text{Size: 22, Style: fontstyle.Bold, Color: rgbColor(TextOn(Theme.Accent)), Align: align.Left, Top: 4, Bottom: 6, Left: 4, Right: 4, VerticalPadding: 1}))
	m.AddAutoRow(band)
	if strings.TrimSpace(subtitle) != "" {
		m.AddAutoRow(text.NewCol(12, subtitle, props.Text{Size: 11, Color: rgbColor(Theme.Muted), Top: 1, Bottom: 3, VerticalPadding: 0.5}))
	}
	m.AddRow(6, text.NewCol(12, "")) // spacer
	return m, nil
}

func (g PDFGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	title, _ := req.Payload["title"].(string)
	blocks := parseBlocks(req.Payload)
	content, _ := req.Payload["content"].(string)
	if len(blocks) == 0 && strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content or blocks are required")
	}
	if len(blocks) == 0 {
		blocks = blocksFromMarkdown(content)
	}
	subtitle, _ := req.Payload["subtitle"].(string)
	if strings.TrimSpace(title) == "" {
		title = "Document"
	}
	// gofpdf (via maroto) indexes a 65536-entry glyph-width table by rune and
	// panics on any code point outside the Basic Multilingual Plane. The bundled
	// Go fonts carry no glyphs for those runes anyway, so strip them before they
	// reach the renderer. See sanitizeForPDF.
	title = sanitizeForPDF(title)
	subtitle = sanitizeForPDF(subtitle)
	for i := range blocks {
		blocks[i] = sanitizeBlock(blocks[i])
	}
	m, err := newMaroto(title, subtitle)
	if err != nil {
		return GeneratedMeta{}, err
	}
	for _, b := range blocks {
		renderBlock(m, b)
	}
	doc, err := m.Generate()
	if err != nil {
		return GeneratedMeta{}, err
	}
	if _, err := w.Write(doc.GetBytes()); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pdf", MIMEType: artifact.MIMEType("pdf")}, nil
}

func (g PDFGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create a styled PDF report. Prefer the structured 'blocks' array (heading, " +
			"paragraph, bullets, table, columns, callout) over a flat text string: use headings to " +
			"structure sections, tables for tabular data, and callouts to emphasize key points. " +
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
							"type":  map[string]any{"type": "string", "enum": []string{"heading", "paragraph", "bullets", "table", "columns", "callout"}},
							"level": map[string]any{"type": "integer"},
							"text":  map[string]any{"type": "string"},
							"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"rows":  map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
							"left":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"right": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
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
