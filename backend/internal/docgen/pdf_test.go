package docgen

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// fakeRenderer is a pdfRenderer stub that records what it was asked to convert.
type fakeRenderer struct {
	gotHTML   string
	gotAssets []gotenbergAsset
	ret       []byte
	err       error
}

func (f *fakeRenderer) Convert(_ context.Context, html string, assets []gotenbergAsset, _ convertOptions) ([]byte, error) {
	f.gotHTML = html
	f.gotAssets = assets
	if f.err != nil {
		return nil, f.err
	}
	return f.ret, nil
}

func genWith(t *testing.T, payload map[string]any) (*fakeRenderer, []byte) {
	t.Helper()
	fake := &fakeRenderer{ret: []byte("%PDF-1.7 fake")}
	var out bytes.Buffer
	meta, err := PDFGenerator{client: fake}.Generate(GenerateRequest{Filename: "f", Payload: payload}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "pdf" || meta.MIMEType != "application/pdf" {
		t.Fatalf("meta = %#v", meta)
	}
	return fake, out.Bytes()
}

func TestPDFGeneratorWritesConvertedBytes(t *testing.T) {
	fake, out := genWith(t, map[string]any{"title": "Quarterly Report", "content": "# Summary\n\nRevenue increased."})
	if !bytes.Equal(out, []byte("%PDF-1.7 fake")) {
		t.Fatalf("output = %q, want the renderer's bytes", out)
	}
	if !strings.Contains(fake.gotHTML, "Quarterly Report") {
		t.Errorf("HTML missing title: %s", fake.gotHTML)
	}
	if len(fake.gotAssets) == 0 {
		t.Error("expected font assets to be uploaded")
	}
}

func TestPDFGeneratorRejectsEmptyContent(t *testing.T) {
	_, err := PDFGenerator{}.Generate(GenerateRequest{Filename: "empty", Payload: map[string]any{"content": ""}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}

func TestRenderHTMLTypedBlocks(t *testing.T) {
	html := renderHTML("Report", "sub", []pdfBlock{
		{Type: "heading", Level: 1, Text: "Summary"},
		{Type: "paragraph", Text: "All good."},
		{Type: "bullets", Items: []string{"one", "two"}},
	})
	for _, want := range []string{`<div class="band"><h1>Report</h1>`, `<p class="subtitle">sub</p>`, "<h2>Summary</h2>", `<p class="body">All good.</p>`, "<ul><li>one</li><li>two</li></ul>"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q\n%s", want, html)
		}
	}
}

func TestRenderHTMLTableColumnsCallout(t *testing.T) {
	html := renderHTML("R", "", []pdfBlock{
		{Type: "table", Rows: [][]string{{"Name", "Value"}, {"A", "1"}}},
		{Type: "columns", Left: []string{"L1"}, Right: []string{"R1"}},
		{Type: "callout", Text: "note"},
	})
	for _, want := range []string{"<table><thead><tr><th>Name</th><th>Value</th></tr></thead>", "<tbody><tr><td>A</td><td>1</td></tr></tbody>", `<div class="cols">`, `<div class="callout">note</div>`} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q\n%s", want, html)
		}
	}
}

func TestRenderHTMLTableRaggedRows(t *testing.T) {
	// A short row must be padded with empty cells so columns stay aligned.
	html := renderHTML("R", "", []pdfBlock{{Type: "table", Rows: [][]string{{"A", "B", "C"}, {"x"}}}})
	if !strings.Contains(html, "<tr><td>x</td><td></td><td></td></tr>") {
		t.Errorf("ragged row not padded to 3 cells:\n%s", html)
	}
}

func TestRenderHTMLEscapes(t *testing.T) {
	html := renderHTML("<script>t", "", []pdfBlock{
		{Type: "paragraph", Text: `<script>alert(1)</script> & "x"`},
		{Type: "table", Rows: [][]string{{"<b>h</b>"}}},
	})
	if strings.Contains(html, "<script>alert(1)") {
		t.Fatalf("unescaped script survived:\n%s", html)
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&amp;", "&lt;b&gt;h&lt;/b&gt;", "&lt;script&gt;t"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing escaped %q", want)
		}
	}
}

func TestRenderHTMLMarkers(t *testing.T) {
	// Markers colour inline, incl. mixed with HTML-special chars and in prose.
	html := renderHTML("T", "", []pdfBlock{
		{Type: "paragraph", Text: "the ✓ mark and ✗ and emoji ✅ ❌ <b>x</b>"},
	})
	if got := strings.Count(html, `<span class="mark-ok">✓</span>`); got != 2 { // ✓ and ✅→✓
		t.Errorf("mark-ok count = %d, want 2\n%s", got, html)
	}
	if got := strings.Count(html, `<span class="mark-no">✗</span>`); got != 2 { // ✗ and ❌→✗
		t.Errorf("mark-no count = %d, want 2", got)
	}
	if !strings.Contains(html, "&lt;b&gt;x&lt;/b&gt;") {
		t.Error("HTML in marker text not escaped")
	}
}

func TestRenderHTMLCodeHighlightAndPlain(t *testing.T) {
	// Known language → chroma inline-style spans (style="color:#..."). Note the
	// document CSS also contains "color:", so key on the chroma inline-style form.
	hi := renderHTML("T", "", []pdfBlock{{Type: "code", Text: "package main", Language: "go"}})
	if !strings.Contains(hi, `<pre class="code"><code>`) || !strings.Contains(hi, `style="color:#`) {
		t.Errorf("expected highlighted code with inline colors:\n%s", hi)
	}
	// No language (ASCII art) → plain, escaped, no color spans, alignment preserved.
	art := "┌─┐\n│ │\n└─┘"
	plain := renderHTML("T", "", []pdfBlock{{Type: "code", Text: art + " <x>"}})
	if strings.Contains(plain, `style="color:#`) {
		t.Errorf("plain code should not be highlighted:\n%s", plain)
	}
	if !strings.Contains(plain, art) || !strings.Contains(plain, "&lt;x&gt;") {
		t.Errorf("plain code not preserved/escaped:\n%s", plain)
	}
}

func TestRenderHTMLPassesNonBMPThrough(t *testing.T) {
	// Unlike the old gofpdf path, non-BMP emoji must now reach the HTML unchanged
	// (Chromium renders them); no replacement character.
	html := renderHTML("T \U0001F600", "", []pdfBlock{{Type: "paragraph", Text: "hi \U0001F680 there"}})
	if !strings.Contains(html, "\U0001F600") || !strings.Contains(html, "\U0001F680") {
		t.Errorf("non-BMP emoji were dropped:\n%s", html)
	}
	if strings.Contains(html, "�") {
		t.Error("unexpected replacement character")
	}
}

func TestPDFSchemaAdvertisesBlocks(t *testing.T) {
	schema := PDFGenerator{}.Schema()
	props, _ := schema.Parameters["properties"].(map[string]any)
	if _, ok := props["blocks"]; !ok {
		t.Fatalf("schema missing blocks: %#v", props)
	}
	if _, ok := props["content"]; !ok {
		t.Fatal("schema should keep content fallback")
	}
	blocks, _ := props["blocks"].(map[string]any)
	items, _ := blocks["items"].(map[string]any)
	bprops, _ := items["properties"].(map[string]any)
	if _, ok := bprops["language"]; !ok {
		t.Errorf("code blocks should advertise a language field: %#v", bprops)
	}
}

func TestParseBlocksReadsLanguage(t *testing.T) {
	blocks := parseBlocks(map[string]any{"blocks": []any{
		map[string]any{"type": "code", "text": "x=1", "language": "python"},
	}})
	if len(blocks) != 1 || blocks[0].Language != "python" {
		t.Fatalf("language not parsed: %#v", blocks)
	}
}
