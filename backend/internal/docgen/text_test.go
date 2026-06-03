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
