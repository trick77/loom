package docgen

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestDOCXGeneratorWritesDocumentPackage(t *testing.T) {
	gen := DOCXGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Filename: "report.docx",
		Payload: map[string]any{
			"content": "# Report\n\nHello from Spark.",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "docx" || meta.MIMEType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("meta = %#v", meta)
	}
	documentXML := zipEntry(t, out.Bytes(), "word/document.xml")
	if !strings.Contains(documentXML, "Hello from Spark.") {
		t.Fatalf("document.xml = %s", documentXML)
	}
}

func TestDOCXGeneratorRejectsEmptyContent(t *testing.T) {
	gen := DOCXGenerator{}
	_, err := gen.Generate(GenerateRequest{
		Filename: "empty.docx",
		Payload:  map[string]any{"content": ""},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}

func zipEntry(t *testing.T, data []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer func() {
			if err := rc.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		}()
		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		return string(content)
	}
	t.Fatalf("missing zip entry %q", name)
	return ""
}
