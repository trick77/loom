package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/signintech/gopdf"
	"github.com/trick77/spark/internal/artifact"
	"golang.org/x/image/font/gofont/goregular"
)

type PDFGenerator struct{}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func (g PDFGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a simple PDF from Markdown or plain text content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"title":    map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "content"},
			"additionalProperties": false,
		},
	}
}

func (g PDFGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4, Unit: gopdf.UnitMM})
	if err := pdf.AddTTFFontData("goregular", goregular.TTF); err != nil {
		return GeneratedMeta{}, err
	}
	pdf.AddPage()
	pdf.SetXY(18, 20)
	if title, ok := req.Payload["title"].(string); ok && strings.TrimSpace(title) != "" {
		if err := pdf.SetFont("goregular", "", 18); err != nil {
			return GeneratedMeta{}, err
		}
		if err := pdf.Cell(nil, strings.TrimSpace(title)); err != nil {
			return GeneratedMeta{}, err
		}
		pdf.Br(10)
	}
	if err := pdf.SetFont("goregular", "", 11); err != nil {
		return GeneratedMeta{}, err
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			if err := pdf.SetFont("goregular", "", 16); err != nil {
				return GeneratedMeta{}, err
			}
			if err := pdf.Cell(nil, strings.TrimSpace(strings.TrimPrefix(line, "# "))); err != nil {
				return GeneratedMeta{}, err
			}
			if err := pdf.SetFont("goregular", "", 11); err != nil {
				return GeneratedMeta{}, err
			}
		} else if strings.TrimSpace(line) != "" {
			if err := pdf.Cell(nil, strings.TrimSpace(line)); err != nil {
				return GeneratedMeta{}, err
			}
		}
		pdf.Br(7)
	}
	if _, err := pdf.WriteTo(w); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pdf", MIMEType: artifact.MIMEType("pdf")}, nil
}
