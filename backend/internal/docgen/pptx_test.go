package docgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

func TestPPTXSchemaAdvertisesLayouts(t *testing.T) {
	schema := PPTXGenerator{}.Schema()
	props, _ := schema.Parameters["properties"].(map[string]any)
	slides, _ := props["slides"].(map[string]any)
	items, _ := slides["items"].(map[string]any)
	itemProps, _ := items["properties"].(map[string]any)
	if _, ok := itemProps["layout"]; !ok {
		t.Fatalf("slides items missing layout: %#v", itemProps)
	}
	if !strings.Contains(schema.Description, "layout") {
		t.Fatalf("description should mention layouts: %q", schema.Description)
	}
}

func TestPPTXAllLayoutsConvertCleanlyWithLibreOffice(t *testing.T) {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		t.Skip("soffice not installed; skipping OOXML validity round-trip")
	}
	var out bytes.Buffer
	_, err = PPTXGenerator{}.Generate(GenerateRequest{
		Filename: "all.pptx",
		Payload: map[string]any{
			"title": "All Layouts",
			"slides": []any{
				map[string]any{"layout": "title", "title": "Deck", "subtitle": "Sub"},
				map[string]any{"layout": "section", "title": "Part I"},
				map[string]any{"layout": "bullets", "title": "Points", "bullets": []any{"one", "two"}},
				map[string]any{"layout": "two-column", "title": "Compare", "columns": map[string]any{"left": []any{"L"}, "right": []any{"R"}}},
				map[string]any{"layout": "big-number", "title": "Growth", "number": "85%", "caption": "YoY"},
				map[string]any{"layout": "quote", "title": "Q", "quote": "Ship it", "attribution": "Jan"},
				map[string]any{"layout": "table", "title": "Data", "table": []any{[]any{"H1", "H2"}, []any{"a", "b"}}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "all.pptx")
	if err := os.WriteFile(src, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(soffice, "--headless", "--convert-to", "pdf", "--outdir", dir, src)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("soffice convert failed: %v\n%s", err, combined)
	}
	if _, err := os.Stat(filepath.Join(dir, "all.pdf")); err != nil {
		t.Fatalf("expected all.pdf, stat error: %v", err)
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
