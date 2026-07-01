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
	"github.com/trick77/loom/internal/artifact"
	"golang.org/x/image/font/gofont/gomono"
)

// PDF font families: the bundled "Loom Sans" typeface for text (the Go typeface
// with symbol glyphs grafted in — see assets.go), Go Mono for code.
const (
	pdfFontFamily = "loom"
	pdfMonoFamily = "loom-mono"
)

// pdfGrid is maroto's column grid sum. 24 (vs the default 12) gives finer
// control — notably a tight one-unit bullet column for hanging-indented lists.
const pdfGrid = 24

type PDFGenerator struct{}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func rgbColor(c RGB) *props.Color { return &props.Color{Red: c.R, Green: c.G, Blue: c.B} }

// newMaroto builds a maroto instance with the Loom fonts and an accent header band.
// Two families are registered — the Go typeface (default, all text) and Go Mono
// (code). gofpdf errors on any (family, style) combo it is asked to render without
// first registering it, so every style each family uses is loaded here.
func newMaroto(title, subtitle string) (core.Maroto, error) {
	fonts, err := repository.New().
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Normal, fontRegular).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Bold, fontBold).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Italic, fontItalic).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.BoldItalic, fontBoldItalic).
		AddUTF8FontFromBytes(pdfMonoFamily, fontstyle.Normal, gomono.TTF).
		Load()
	if err != nil {
		return nil, err
	}
	cfg := config.NewBuilder().
		WithCustomFonts(fonts).
		WithMaxGridSize(pdfGrid).
		WithDefaultFont(&props.Font{Family: pdfFontFamily}).
		Build()
	m := maroto.New(cfg)

	band := col.New(pdfGrid).WithStyle(&props.Cell{BackgroundColor: rgbColor(Theme.Accent)})
	band.Add(text.New(title, props.Text{Size: 22, Style: fontstyle.Bold, Color: rgbColor(TextOn(Theme.Accent)), Align: align.Left, Top: 4, Bottom: 6, Left: 4, Right: 4, VerticalPadding: 1}))
	m.AddAutoRow(band)
	if strings.TrimSpace(subtitle) != "" {
		m.AddAutoRow(text.NewCol(pdfGrid, subtitle, props.Text{Size: 11, Color: rgbColor(Theme.Muted), Top: 1, Bottom: 3, VerticalPadding: 0.5}))
	}
	m.AddRow(6, text.NewCol(pdfGrid, "")) // spacer
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
	for i, b := range blocks {
		// Bullet rows are spaced tightly, so a callout immediately after a list would
		// otherwise crowd it — add a little breathing room above the box in that case.
		if b.Type == "callout" && i > 0 && blocks[i-1].Type == "bullets" {
			m.AddRow(3, text.NewCol(pdfGrid, ""))
		}
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
			"paragraph, bullets, table, columns, callout, code) over a flat text string: use headings to " +
			"structure sections, tables for tabular data, callouts to emphasize key points, and code " +
			"blocks (rendered monospaced) for code samples or terminal output — put the code in 'text'. " +
			"Start a table cell or bullet with ✓ or ✗ to show a green-check / red-cross status marker " +
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
							"type":  map[string]any{"type": "string", "enum": []string{"heading", "paragraph", "bullets", "table", "columns", "callout", "code"}},
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
