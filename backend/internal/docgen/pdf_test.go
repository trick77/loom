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
