package docgen

import (
	"bytes"
	"testing"
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
