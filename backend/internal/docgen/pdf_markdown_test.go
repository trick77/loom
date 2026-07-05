package docgen

import "strings"
import "testing"

func TestRenderMarkdownBodyRendersFormatting(t *testing.T) {
	md := "### Findings\n\n" +
		"Some **bold** and *italic* and `inline` text.\n\n" +
		"1. first\n2. second\n\n" +
		"> a quote\n\n" +
		"| Name | Value |\n| --- | --- |\n| A | 1 |\n\n" +
		"```go\npackage main\n```\n"
	html := renderMarkdownBody(md)

	// Structural markdown must render as real HTML elements, not literal tokens.
	for _, want := range []string{
		`<div class="md">`,
		"<h3>Findings</h3>",
		"<strong>bold</strong>",
		"<em>italic</em>",
		"<code>inline</code>",
		"<ol>",
		"<li>first</li>",
		"<blockquote>",
		"<table>",
		"<th>Name</th>",
		"<td>A</td>",
		`<pre class="code"><code>`, // fenced code reuses the blocks-path wrapper
	} {
		if !strings.Contains(html, want) {
			t.Errorf("markdown body missing %q\n%s", want, html)
		}
	}

	// The raw markdown tokens must NOT survive as literal text (the reported bug).
	for _, bad := range []string{"### Findings", "**bold**", "```go", "| Name |"} {
		if strings.Contains(html, bad) {
			t.Errorf("raw markdown token leaked into output: %q\n%s", bad, html)
		}
	}

	// Fenced code with a known language is syntax-highlighted (chroma inline colours).
	if !strings.Contains(html, `style="color:#`) {
		t.Errorf("expected highlighted fenced code with inline colours:\n%s", html)
	}
}

func TestRenderMarkdownBodyColoursMarkers(t *testing.T) {
	html := renderMarkdownBody("Status: ✓ done and ✗ blocked and ✅ ok")
	if got := strings.Count(html, `<span class="mark-ok">✓</span>`); got != 2 { // ✓ and ✅→✓
		t.Errorf("mark-ok count = %d, want 2\n%s", got, html)
	}
	if !strings.Contains(html, `<span class="mark-no">✗</span>`) {
		t.Errorf("cross marker not coloured:\n%s", html)
	}
}

func TestRenderMarkdownBodyDropsRawHTML(t *testing.T) {
	// Raw HTML in the source must not survive (WithUnsafe is deliberately off),
	// matching the escape-everything posture of the blocks path.
	html := renderMarkdownBody("hello <script>alert(1)</script> world")
	if strings.Contains(html, "<script>alert(1)") {
		t.Errorf("raw HTML survived markdown render:\n%s", html)
	}
}

func TestPDFFallbackRoutesThroughMarkdown(t *testing.T) {
	// End-to-end via Generate: a content-only payload must reach the renderer as a
	// styled .md body, not the old literal line-parse.
	fake, _ := genWith(t, map[string]any{"content": "## Heading\n\n**bold** body"})
	if !strings.Contains(fake.gotHTML, `<div class="md">`) {
		t.Errorf("content fallback did not use the markdown body:\n%s", fake.gotHTML)
	}
	if !strings.Contains(fake.gotHTML, "<strong>bold</strong>") {
		t.Errorf("inline formatting not rendered on fallback:\n%s", fake.gotHTML)
	}
}

func TestPDFUnparseableBlocksGivesSpecificError(t *testing.T) {
	// blocks present but yielding zero usable items, and no content → the error
	// must pinpoint the cause rather than the generic "content or blocks are required".
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "f",
		Payload:  map[string]any{"blocks": "not valid json"},
	}, &discardWriter{})
	if err == nil {
		t.Fatal("expected an error for unparseable blocks")
	}
	if !strings.Contains(err.Error(), "none were parseable") {
		t.Errorf("error should identify unparseable blocks, got: %v", err)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
