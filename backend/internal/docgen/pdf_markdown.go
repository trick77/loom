package docgen

import (
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/util"
)

// markdownConverter renders the `content` fallback (GitHub-flavored Markdown) to
// HTML. Raw HTML in the source is dropped (WithUnsafe is deliberately NOT set),
// matching the escape-everything posture of the typed `blocks` path. Fenced code
// is highlighted with the same chroma style and wrapped in the same
// <pre class="code"><code> element as highlightCode (pdf_html.go), so the fenced
// and typed code paths render identically.
var markdownConverter = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM, // tables, strikethrough, autolinks, task lists
		highlighting.NewHighlighting(
			highlighting.WithStyle(codeHighlightStyle),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(false),          // inline colour styles, no external stylesheet
				chromahtml.PreventSurroundingPre(true), // we supply the <pre> wrapper below
			),
			highlighting.WithWrapperRenderer(func(w util.BufWriter, _ highlighting.CodeBlockContext, entering bool) {
				if entering {
					_, _ = w.WriteString(`<pre class="code"><code>`)
				} else {
					_, _ = w.WriteString(`</code></pre>`)
				}
			}),
		),
	),
)

// renderMarkdownBody converts the markdown content string into the body HTML
// injected into the PDF document shell. The result is wrapped in a `.md` scope so
// its bare tags (p, h1-h6, ol, blockquote, …) can be styled without clobbering the
// blocks-path CSS, and passed through markers() so ✓/✗ status glyphs are coloured
// exactly as on the blocks path.
func renderMarkdownBody(content string) string {
	var buf strings.Builder
	if err := markdownConverter.Convert([]byte(content), &buf); err != nil {
		// Never ship a broken document: fall back to the escaped raw content.
		return `<div class="md"><p>` + text2html(content) + `</p></div>`
	}
	return `<div class="md">` + markers(buf.String()) + `</div>`
}
