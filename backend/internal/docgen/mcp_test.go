package docgen

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestFileName(t *testing.T) {
	for _, tc := range []struct{ display, ext, want string }{
		{"notes", "md", "notes.md"},
		{"notes.md", "md", "notes.md"},
		{"Report.PDF", "pdf", "Report.PDF"},
		{"notes.txt", "md", "notes.md"},
		{"Q3 v1.2 summary", "pdf", "Q3 v1.2 summary.pdf"},
		{"report.final", "docx", "report.final.docx"},
		{"/tmp/out/q3 report", "pdf", "q3 report.pdf"},
		{`C:\docs\deck`, "pptx", "deck.pptx"},
		{"", "xlsx", "artifact.xlsx"},
		{"  ", "docx", "artifact.docx"},
		{"..", "pdf", "artifact.pdf"},
		{".pdf", "pdf", "artifact.pdf"},
	} {
		if got := fileName(tc.display, tc.ext); got != tc.want {
			t.Errorf("fileName(%q, %q) = %q, want %q", tc.display, tc.ext, got, tc.want)
		}
	}
}

// emptyGenerator writes nothing and reports success.
type emptyGenerator struct{ TextGenerator }

func (emptyGenerator) Generate(GenerateRequest, io.Writer) (GeneratedMeta, error) {
	return GeneratedMeta{Extension: "txt", MIMEType: MIMEType("txt")}, nil
}

func TestGenerateResults(t *testing.T) {
	ctx := context.Background()
	got := generate(ctx, TextGenerator{}, json.RawMessage(`{"filename":"a/b/notes","extension":"md","content":"# Hi"}`))
	if got.IsError || len(got.Files) != 1 || got.Text != "" {
		t.Fatalf("result %+v", got)
	}
	f := got.Files[0]
	if f.Name != "notes.md" || f.MimeType != "text/markdown; charset=utf-8" || string(f.Data) != "# Hi" {
		t.Fatalf("file %+v", f)
	}
	for name, tc := range map[string]struct {
		gen  Generator
		raw  string
		want string
	}{
		"bad json":   {TextGenerator{}, `[1]`, "invalid arguments"},
		"no content": {TextGenerator{}, `null`, "content is required"},
		"empty":      {emptyGenerator{}, `{}`, "empty"},
	} {
		got := generate(ctx, tc.gen, json.RawMessage(tc.raw))
		if !got.IsError || !strings.Contains(got.Text, tc.want) {
			t.Errorf("%s: %+v, want error with %q", name, got, tc.want)
		}
	}
}

func TestMCPServerServesEveryGenerator(t *testing.T) {
	srv := MCPServer([]Generator{TextGenerator{}, XLSXGenerator{}}, "test")
	if srv.Name != ServerName || srv.MaxBlobBytes != MaxFileBytes || len(srv.Tools) != 2 {
		t.Fatalf("server %+v", srv)
	}
	tool := srv.Tools[1]
	if tool.Name != "create_xlsx_file" || tool.MaxArgBytes < 6*MaxGeneratedInputBytes || tool.Schema["type"] != "object" {
		t.Fatalf("tool %+v", tool)
	}
	if res := tool.Run(context.Background(), json.RawMessage(`{"filename":"t","rows":[["a"]]}`)); res.IsError || len(res.Files) != 1 || res.Files[0].Name != "t.xlsx" {
		t.Fatalf("run %+v", res)
	}
}

func TestExtensionsHaveTypes(t *testing.T) {
	exts := Extensions()
	if len(exts) != 4+len(allowedTextExtensions) {
		t.Fatalf("extensions %v", exts)
	}
	if MIMEType("pdf") != "application/pdf" || MIMEType("yaml") != "text/plain; charset=utf-8" {
		t.Fatal("types")
	}
}
