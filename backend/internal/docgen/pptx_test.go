package docgen

import (
	"bytes"
	"strings"
	"testing"
)

func TestPPTXGeneratorWritesPresentationPackage(t *testing.T) {
	gen := PPTXGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Filename: "deck.pptx",
		Payload: map[string]any{
			"title": "Roadmap",
			"slides": []any{
				map[string]any{"title": "Phase 1", "bullets": []any{"Plan", "Build"}},
				map[string]any{"title": "Phase 2", "bullets": []any{"Verify"}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "pptx" || meta.MIMEType != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("meta = %#v", meta)
	}
	slideXML := zipEntry(t, out.Bytes(), "ppt/slides/slide1.xml")
	if !strings.Contains(slideXML, "Phase 1") || !strings.Contains(slideXML, "Build") {
		t.Fatalf("slide1.xml = %s", slideXML)
	}
}

func TestPPTXGeneratorRejectsEmptySlides(t *testing.T) {
	gen := PPTXGenerator{}
	_, err := gen.Generate(GenerateRequest{
		Filename: "empty.pptx",
		Payload:  map[string]any{"slides": []any{}},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}
