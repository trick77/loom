package docgen

import (
	"fmt"
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Font asset filenames, referenced by @font-face in the CSS and uploaded to
// Gotenberg as multipart assets (see assets.go / fontAssets).
const (
	fontRegularFile  = "Go-Regular.ttf"
	fontBoldFile     = "Go-Bold.ttf"
	fontItalicFile   = "Go-Italic.ttf"
	fontMonoFile     = "JetBrainsMono-Regular.ttf" // sans monospace for code (Go Mono is slab-serif)
	fontMonoBoldFile = "JetBrainsMono-Bold.ttf"
)

// markerDangerHex is the red used for ✗ markers. The Theme has no danger colour,
// so it is fixed here (kept close to the palette's warmth).
const markerDangerHex = "C0392B"

// codeHighlightStyle is a light chroma style that reads on the cream code panel.
const codeHighlightStyle = "github"

// esc HTML-escapes model-provided text exactly once. Every string that reaches
// the output goes through here (or through chroma, which escapes its own input).
func esc(s string) string { return html.EscapeString(s) }

// markers colours ✓/✗ status glyphs inline. It runs over already-escaped text;
// the <span> tags it emits are the only raw HTML it introduces, and they wrap
// safe characters. Every check/cross variant (✅ ❌ ✔ ☑ …) is normalized to the
// canonical brand glyph here.
func markers(escaped string) string {
	var b strings.Builder
	b.Grow(len(escaped))
	for _, r := range escaped {
		switch markerNormalize[r] {
		case checkRune:
			b.WriteString(`<span class="mark-ok">✓</span>`)
		case crossRune:
			b.WriteString(`<span class="mark-no">✗</span>`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// text2html escapes then colours markers — the standard path for any body text.
func text2html(s string) string { return markers(esc(s)) }

// highlightCode returns highlighted HTML for a code block. Highlighting only runs
// when an explicit, known language is given — this keeps ASCII-art/diagrams (no
// language) rendered as clean plain monospace instead of mis-tokenized colour.
// The result is trusted HTML (chroma escapes its own input). Falls back to plain
// escaped text on any error.
func highlightCode(source, lang string) string {
	lexer := lexers.Get(strings.TrimSpace(lang))
	if lexer == nil {
		return esc(source)
	}
	lexer = chroma.Coalesce(lexer)
	formatter := chromahtml.New(chromahtml.WithClasses(false), chromahtml.PreventSurroundingPre(true))
	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return esc(source)
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, styles.Get(codeHighlightStyle), it); err != nil {
		return esc(source)
	}
	return buf.String()
}

// renderHTML builds the full HTML document sent to Gotenberg.
func renderHTML(title, subtitle string, blocks []pdfBlock) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<style>")
	b.WriteString(pdfCSS())
	b.WriteString("</style></head><body>")

	b.WriteString(`<div class="band"><h1>` + esc(title) + `</h1></div>`)
	if strings.TrimSpace(subtitle) != "" {
		b.WriteString(`<p class="subtitle">` + esc(subtitle) + `</p>`)
	}
	for _, blk := range blocks {
		renderHTMLBlock(&b, blk)
	}
	b.WriteString("</body></html>")
	return b.String()
}

func renderHTMLBlock(b *strings.Builder, blk pdfBlock) {
	switch blk.Type {
	case "heading":
		if blk.Level >= 2 {
			b.WriteString("<h3>" + text2html(blk.Text) + "</h3>")
		} else {
			b.WriteString("<h2>" + text2html(blk.Text) + "</h2>")
		}
	case "bullets":
		b.WriteString("<ul>")
		for _, it := range blk.Items {
			b.WriteString("<li>" + text2html(it) + "</li>")
		}
		b.WriteString("</ul>")
	case "table":
		renderHTMLTable(b, blk.Rows)
	case "columns":
		b.WriteString(`<div class="cols"><div>`)
		for _, l := range blk.Left {
			b.WriteString("<p>" + text2html(l) + "</p>")
		}
		b.WriteString(`</div><div>`)
		for _, r := range blk.Right {
			b.WriteString("<p>" + text2html(r) + "</p>")
		}
		b.WriteString(`</div></div>`)
	case "callout":
		b.WriteString(`<div class="callout">` + text2html(blk.Text) + `</div>`)
	case "code":
		b.WriteString(`<pre class="code"><code>` + highlightCode(blk.Text, blk.Language) + `</code></pre>`)
	default: // paragraph
		b.WriteString(`<p class="body">` + text2html(blk.Text) + `</p>`)
	}
}

func renderHTMLTable(b *strings.Builder, rows [][]string) {
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}
	cell := func(row []string, i, n int) {
		v := ""
		if i < len(row) {
			v = row[i]
		}
		b.WriteString("<" + colTag(n) + ">" + text2html(v) + "</" + colTag(n) + ">")
	}
	b.WriteString("<table>")
	for ri, r := range rows {
		if ri == 0 {
			b.WriteString("<thead><tr>")
			for ci := 0; ci < cols; ci++ {
				cell(r, ci, 0) // header row → <th>
			}
			b.WriteString("</tr></thead><tbody>")
			continue
		}
		b.WriteString("<tr>")
		for ci := 0; ci < cols; ci++ {
			cell(r, ci, 1) // body → <td>
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
}

func colTag(n int) string {
	if n == 0 {
		return "th"
	}
	return "td"
}

// pdfCSS builds the stylesheet, sourcing colours from the shared Theme palette.
func pdfCSS() string {
	onAccent := textOnHex(Theme.AccentHex)
	return fmt.Sprintf(`
@font-face{font-family:"Loom Sans";font-weight:normal;font-style:normal;src:url(%q)}
@font-face{font-family:"Loom Sans";font-weight:bold;font-style:normal;src:url(%q)}
@font-face{font-family:"Loom Sans";font-weight:normal;font-style:italic;src:url(%q)}
@font-face{font-family:"Loom Mono";font-weight:normal;src:url(%q)}
@font-face{font-family:"Loom Mono";font-weight:bold;src:url(%q)}
:root{
 --ink:#%s; --cream:#%s; --cream-alt:#%s; --accent:#%s;
 --sage:#%s; --muted:#%s; --callout:#%s; --on-accent:#%s; --danger:#%s;
}
*{box-sizing:border-box}
body{font-family:"Loom Sans",system-ui,sans-serif;color:var(--ink);background:#fff;font-size:13px;line-height:1.45;margin:0}
.band{background:var(--accent);color:var(--on-accent);padding:10px 12px;border-radius:2px}
.band h1{margin:0;font-size:25px;font-weight:bold}
.subtitle{color:var(--muted);margin:6px 2px 2px}
h2{color:var(--accent);font-size:19px;margin:14px 0 4px;break-after:avoid}
h3{color:var(--accent);font-size:15px;margin:10px 0 3px;break-after:avoid}
p.body{margin:3px 0}
ul{margin:4px 0;padding-left:1.2em}
li{margin:2px 0}
table{width:100%%;border-collapse:collapse;table-layout:fixed;margin:6px 0}
th,td{padding:4px 6px;text-align:left;vertical-align:top;overflow-wrap:anywhere;word-break:normal}
thead{display:table-header-group}
th{background:var(--accent);color:var(--on-accent);font-weight:bold}
tbody tr{break-inside:avoid}
tbody tr:nth-child(odd) td{background:var(--cream)}
tbody tr:nth-child(even) td{background:var(--cream-alt)}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin:6px 0}
.cols p{margin:2px 0}
.callout{background:var(--callout);color:var(--accent);font-style:italic;padding:8px 12px;margin:8px 0;border-radius:2px;break-inside:avoid}
pre.code{font-family:"Loom Mono",ui-monospace,monospace;font-size:11px;line-height:1.4;background:var(--cream-alt);color:var(--ink);padding:8px 10px;margin:6px 0;white-space:pre;overflow:hidden;border-radius:2px;break-inside:avoid}
pre.code code{font-family:inherit;background:none}
.mark-ok{color:var(--sage);font-weight:600}
.mark-no{color:var(--danger);font-weight:600}
`,
		fontRegularFile, fontBoldFile, fontItalicFile, fontMonoFile, fontMonoBoldFile,
		Theme.InkHex, Theme.CreamHex, Theme.CreamAltHex, Theme.AccentHex,
		Theme.SageHex, Theme.MutedHex, Theme.CalloutHex, onAccent, markerDangerHex,
	)
}
