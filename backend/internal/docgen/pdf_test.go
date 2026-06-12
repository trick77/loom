package docgen

import (
	"bytes"
	"testing"

	"github.com/johnfercher/go-tree/node"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/core/entity"
	"github.com/johnfercher/maroto/v2/pkg/metrics"
)

func TestPDFGeneratorWritesPDF(t *testing.T) {
	gen := PDFGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Filename: "report.pdf",
		Payload: map[string]any{
			"title":   "Quarterly Report",
			"content": "# Summary\n\nRevenue increased.",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "pdf" || meta.MIMEType != "application/pdf" {
		t.Fatalf("meta = %#v", meta)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("output does not look like PDF: %q", out.Bytes()[:min(out.Len(), 16)])
	}
}

func TestPDFGeneratorRejectsEmptyContent(t *testing.T) {
	gen := PDFGenerator{}
	_, err := gen.Generate(GenerateRequest{
		Filename: "empty.pdf",
		Payload:  map[string]any{"content": ""},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}

func TestPDFRendersTypedBlocks(t *testing.T) {
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "r.pdf",
		Payload: map[string]any{
			"title": "Report",
			"blocks": []any{
				map[string]any{"type": "heading", "level": float64(1), "text": "Summary"},
				map[string]any{"type": "paragraph", "text": "All good."},
				map[string]any{"type": "bullets", "items": []any{"one", "two", "three"}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", out.Bytes()[:8])
	}
}

func TestPDFRendersTableColumnsCallout(t *testing.T) {
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "r.pdf",
		Payload: map[string]any{
			"title": "Report",
			"blocks": []any{
				map[string]any{"type": "table", "rows": []any{[]any{"Name", "Value"}, []any{"A", "1"}, []any{"B", "2"}}},
				map[string]any{"type": "columns", "left": []any{"L1"}, "right": []any{"R1"}},
				map[string]any{"type": "callout", "text": "Important note"},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF")
	}
}

func TestPDFTextBlocksUseAutoRows(t *testing.T) {
	blocks := []pdfBlock{
		{Type: "heading", Level: 1, Text: "A heading long enough that it may wrap in the PDF output"},
		{Type: "paragraph", Text: "A paragraph long enough that it may wrap across multiple rendered lines in the PDF output."},
		{Type: "bullets", Items: []string{"A bullet long enough that it may wrap across multiple rendered lines in the PDF output."}},
		{Type: "table", Rows: [][]string{{"Column", "Description"}, {"A", "A table cell long enough that it may wrap across multiple rendered lines."}}},
		{Type: "columns", Left: []string{"A left column value long enough to wrap."}, Right: []string{"A right column value long enough to wrap."}},
		{Type: "callout", Text: "A callout long enough that it may wrap across multiple rendered lines in the PDF output."},
	}

	m := &recordingMaroto{}

	for _, block := range blocks {
		renderBlock(m, block)
	}

	if m.addRowCalls != 0 {
		t.Fatalf("AddRow calls = %d, want 0 for text-bearing blocks", m.addRowCalls)
	}
	if m.addAutoRowCalls != 7 {
		t.Fatalf("AddAutoRow calls = %d, want 7", m.addAutoRowCalls)
	}
}

type recordingMaroto struct {
	addRowCalls     int
	addAutoRowCalls int
}

func (m *recordingMaroto) RegisterHeader(...core.Row) error { return nil }
func (m *recordingMaroto) RegisterFooter(...core.Row) error { return nil }
func (m *recordingMaroto) AddRows(...core.Row)              {}
func (m *recordingMaroto) AddRow(float64, ...core.Col) core.Row {
	m.addRowCalls++
	return nil
}
func (m *recordingMaroto) AddAutoRow(...core.Col) core.Row {
	m.addAutoRowCalls++
	return nil
}
func (m *recordingMaroto) FitlnCurrentPage(float64) bool            { return true }
func (m *recordingMaroto) GetCurrentConfig() *entity.Config         { return nil }
func (m *recordingMaroto) AddPages(...core.Page)                    {}
func (m *recordingMaroto) GetStructure() *node.Node[core.Structure] { return nil }
func (m *recordingMaroto) Generate() (core.Document, error)         { return recordingDocument{}, nil }
func (d recordingDocument) GetBytes() []byte                        { return nil }
func (d recordingDocument) GetBase64() string                       { return "" }
func (d recordingDocument) Save(string) error                       { return nil }
func (d recordingDocument) GetReport() *metrics.Report              { return nil }
func (d recordingDocument) Merge([]byte) error                      { return nil }

type recordingDocument struct{}

func TestPDFSchemaAdvertisesBlocks(t *testing.T) {
	schema := PDFGenerator{}.Schema()
	props, _ := schema.Parameters["properties"].(map[string]any)
	if _, ok := props["blocks"]; !ok {
		t.Fatalf("schema missing blocks: %#v", props)
	}
	if _, ok := props["content"]; !ok {
		t.Fatalf("schema should keep content fallback")
	}
}

func TestPDFHandlesNonBMPCharacters(t *testing.T) {
	// gofpdf (via maroto) indexes a 65536-entry width table by rune. A character
	// at exactly U+10000 panicked with "index out of range [65536] with length
	// 65536"; higher non-BMP runes (emoji) panic during font subset embedding.
	// Such runes reach the generator from model-produced tool arguments (a JSON
	// surrogate pair 𐀀 decodes to U+10000). Every text path must cope.
	nonBMP := "X\U00010000Y\U0001F600Z" // U+10000 + grinning face emoji
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "unicode.pdf",
		Payload: map[string]any{
			"title":    "Title " + nonBMP,
			"subtitle": "Sub " + nonBMP,
			"blocks": []any{
				map[string]any{"type": "heading", "level": float64(1), "text": "Heading " + nonBMP},
				map[string]any{"type": "paragraph", "text": "Para " + nonBMP},
				map[string]any{"type": "bullets", "items": []any{"Bullet " + nonBMP}},
				map[string]any{"type": "table", "rows": []any{[]any{"Head " + nonBMP}, []any{"Cell " + nonBMP}}},
				map[string]any{"type": "columns", "left": []any{"L " + nonBMP}, "right": []any{"R " + nonBMP}},
				map[string]any{"type": "callout", "text": "Note " + nonBMP},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF")
	}
}

func TestPDFHandlesNonBMPInMarkdownContent(t *testing.T) {
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "unicode.pdf",
		Payload:  map[string]any{"title": "T", "content": "# Heading \U00010000\n\nBody \U0001F600 text."},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF")
	}
}

func TestPDFLongContentSpansMultiplePages(t *testing.T) {
	blocks := make([]any, 0, 120)
	for i := 0; i < 120; i++ {
		blocks = append(blocks, map[string]any{"type": "paragraph", "text": "Line of body content that fills vertical space."})
	}
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "long.pdf",
		Payload:  map[string]any{"title": "Long", "blocks": blocks},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// A multi-page PDF declares more than one /Page object.
	if n := bytes.Count(out.Bytes(), []byte("/Type /Page")); n < 2 {
		if n2 := bytes.Count(out.Bytes(), []byte("/Type/Page")); n2 < 2 {
			t.Fatalf("expected multiple pages, found markers %d/%d", n, n2)
		}
	}
}
