package docgen

import (
	"bytes"
	"testing"
)

func TestTextGeneratorWritesUTF8Content(t *testing.T) {
	gen := TextGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Format:   "text",
		Filename: "notes.md",
		Payload: map[string]any{
			"content":   "# Hello\n\nGruezi",
			"extension": "md",
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if out.String() != "# Hello\n\nGruezi" {
		t.Fatalf("output = %q", out.String())
	}
	if meta.Extension != "md" || meta.MIMEType != "text/markdown; charset=utf-8" {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestTextGeneratorInfersExtensionFromFilename(t *testing.T) {
	gen := TextGenerator{}
	meta, err := gen.Generate(GenerateRequest{
		Filename: "report.md",
		Payload:  map[string]any{"content": "# Title"},
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "md" || meta.MIMEType != "text/markdown; charset=utf-8" {
		t.Fatalf("meta = %#v, want md/text-markdown inferred from filename", meta)
	}
}

func TestTextGeneratorDefaultsToTxtForUnknownExtension(t *testing.T) {
	gen := TextGenerator{}
	meta, err := gen.Generate(GenerateRequest{
		Filename: "archive.zip",
		Payload:  map[string]any{"content": "data"},
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "txt" || meta.MIMEType != "text/plain; charset=utf-8" {
		t.Fatalf("meta = %#v, want txt fallback for unknown filename extension", meta)
	}
}

func TestTextGeneratorExplicitExtensionWinsOverFilename(t *testing.T) {
	gen := TextGenerator{}
	meta, err := gen.Generate(GenerateRequest{
		Filename: "report.md",
		Payload:  map[string]any{"content": "x", "extension": "csv"},
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "csv" {
		t.Fatalf("meta = %#v, want explicit extension csv", meta)
	}
}

func TestTextGeneratorRejectsOversizeContent(t *testing.T) {
	gen := TextGenerator{MaxInputBytes: 4}
	_, err := gen.Generate(GenerateRequest{
		Filename: "notes.txt",
		Payload:  map[string]any{"content": "12345"},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}
