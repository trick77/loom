package docgen

import (
	"encoding/json"
	"strings"

	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

type pdfBlock struct {
	Type  string // heading | paragraph | bullets | table | columns | callout
	Level int
	Text  string
	Items []string
	Rows  [][]string
	Left  []string
	Right []string
}

// parseBlocks reads the optional typed "blocks" array. Returns nil if absent.
func parseBlocks(payload map[string]any) []pdfBlock {
	raw, ok := payload["blocks"].([]any)
	if !ok {
		// MiMo frequently serializes the blocks array as a JSON-encoded string
		// (blocks: "[{...}]") rather than a real array. Decode that form instead of
		// silently dropping the entire document and failing "content or blocks are
		// required".
		s, isStr := payload["blocks"].(string)
		if !isStr || strings.TrimSpace(s) == "" {
			return nil
		}
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil
		}
	}
	var out []pdfBlock
	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			continue
		}
		b := pdfBlock{Type: strings.TrimSpace(stringField(item, "type")), Text: strings.TrimSpace(stringField(item, "text"))}
		if lvl, ok := item["level"].(float64); ok {
			b.Level = int(lvl)
		}
		b.Items, _ = stringList(item["items"], "items")
		b.Rows = stringMatrix(item["rows"])
		b.Left, _ = stringList(item["left"], "left")
		b.Right, _ = stringList(item["right"], "right")
		if b.Type != "" {
			out = append(out, b)
		}
	}
	return out
}

// blocksFromMarkdown converts a plain content string into heading/paragraph/bullets blocks.
func blocksFromMarkdown(content string) []pdfBlock {
	var out []pdfBlock
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "## "):
			out = append(out, pdfBlock{Type: "heading", Level: 2, Text: strings.TrimSpace(t[3:])})
		case strings.HasPrefix(t, "# "):
			out = append(out, pdfBlock{Type: "heading", Level: 1, Text: strings.TrimSpace(t[2:])})
		case strings.HasPrefix(t, "- "):
			out = append(out, pdfBlock{Type: "bullets", Items: []string{strings.TrimSpace(t[2:])}})
		default:
			out = append(out, pdfBlock{Type: "paragraph", Text: t})
		}
	}
	return out
}

// sanitizeForPDF replaces every code point outside the Basic Multilingual Plane
// (rune > 0xFFFF) with the Unicode replacement character. gofpdf — which maroto
// uses to render PDFs — looks up glyph widths in a 65536-entry table indexed by
// rune, so a non-BMP rune (an emoji, or a U+10000+ character the model may emit
// via a JSON surrogate pair) panics with "index out of range". The bundled Go
// fonts have no glyphs for those runes anyway, so nothing renderable is lost.
func sanitizeForPDF(s string) string {
	if s == "" {
		return s
	}
	hasNonBMP := false
	for _, r := range s {
		if r > 0xFFFF {
			hasNonBMP = true
			break
		}
	}
	if !hasNonBMP {
		return s
	}
	runes := []rune(s)
	for i, r := range runes {
		if r > 0xFFFF {
			runes[i] = '�'
		}
	}
	return string(runes)
}

// sanitizeBlock applies sanitizeForPDF to every text field of a block. maroto
// only wraps lines on spaces — it renders a literal "\n" as the font's .notdef
// box — so for flowing text we collapse newlines to spaces and let maroto wrap.
// Code blocks keep their newlines; renderBlock splits them into rows itself.
func sanitizeBlock(b pdfBlock) pdfBlock {
	clean := func(s string) string {
		s = sanitizeForPDF(s)
		if b.Type != "code" {
			s = strings.ReplaceAll(s, "\n", " ")
		}
		return s
	}
	b.Text = clean(b.Text)
	for i := range b.Items {
		b.Items[i] = clean(b.Items[i])
	}
	for ri := range b.Rows {
		for ci := range b.Rows[ri] {
			b.Rows[ri][ci] = clean(b.Rows[ri][ci])
		}
	}
	for i := range b.Left {
		b.Left[i] = clean(b.Left[i])
	}
	for i := range b.Right {
		b.Right[i] = clean(b.Right[i])
	}
	return b
}

// nbsp is U+00A0; all three PDF font families carry the glyph (verified).
const nbsp = " "

// preserveIndent converts a line's leading spaces to non-breaking spaces so
// maroto's space-based word-wrap doesn't collapse code indentation. NBSP is in
// all three font families (verified), so it renders, not as a .notdef box.
func preserveIndent(s string) string {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i == 0 {
		return s
	}
	return strings.Repeat(nbsp, i) + s[i:]
}

// renderBlock appends maroto rows for one block.
func renderBlock(m core.Maroto, b pdfBlock) {
	switch b.Type {
	case "heading":
		size := 16.0
		if b.Level >= 2 {
			size = 13.0
		}
		m.AddAutoRow(text.NewCol(pdfGrid, b.Text, props.Text{Size: size, Style: fontstyle.Bold, Color: rgbColor(Theme.Accent), Top: 2, Bottom: 2, VerticalPadding: 0.5}))
	case "bullets":
		// Hanging indent: bullet in a one-unit column, text in the rest, so wrapped
		// lines align under the text. Bullet is full-size and top-matched to the text
		// so it sits on the same line; Bottom is tight to keep items close together.
		for _, it := range b.Items {
			m.AddAutoRow(
				text.NewCol(1, "•", props.Text{Size: 11, Align: align.Right, Color: rgbColor(Theme.Ink), Top: 1, Bottom: 1, Right: 1.5, VerticalPadding: 0.5}),
				text.NewCol(pdfGrid-1, it, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1, Bottom: 1, Left: 1.5, VerticalPadding: 0.5}),
			)
		}
	case "table":
		// Column count is the widest row, so columns align even when rows are ragged
		// (matches the PPTX table renderer).
		cols := 0
		for _, r := range b.Rows {
			if len(r) > cols {
				cols = len(r)
			}
		}
		if cols == 0 {
			break
		}
		span := pdfGrid / cols
		if span == 0 {
			span = 1
		}
		for ri, r := range b.Rows {
			header := ri == 0
			cellStyle := &props.Cell{BackgroundColor: rgbColor(Theme.Cream)}
			textColor := rgbColor(Theme.Ink)
			style := fontstyle.Normal
			if header {
				cellStyle = &props.Cell{BackgroundColor: rgbColor(Theme.Accent)}
				textColor = rgbColor(TextOn(Theme.Accent))
				style = fontstyle.Bold
			} else if ri%2 == 0 {
				cellStyle = &props.Cell{BackgroundColor: rgbColor(Theme.CreamAlt)}
			}
			cells := make([]core.Col, 0, cols)
			for ci := 0; ci < cols; ci++ {
				cell := ""
				if ci < len(r) {
					cell = r[ci]
				}
				cells = append(cells, text.NewCol(span, cell, props.Text{Size: 10, Style: style, Color: textColor, Top: 1, Bottom: 2, Left: 2, Right: 2, VerticalPadding: 0.5}).WithStyle(cellStyle))
			}
			m.AddAutoRow(cells...)
		}
	case "columns":
		// One row per line so the two lists stay side by side; maroto can't break
		// on "\n", so each list entry is rendered as its own row (padding the
		// shorter side) rather than joined with newlines.
		n := len(b.Left)
		if len(b.Right) > n {
			n = len(b.Right)
		}
		colText := func() props.Text {
			return props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1, Bottom: 3, Left: 2, Right: 2, VerticalPadding: 0.5}
		}
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(b.Left) {
				l = b.Left[i]
			}
			if i < len(b.Right) {
				r = b.Right[i]
			}
			m.AddAutoRow(text.NewCol(pdfGrid/2, l, colText()), text.NewCol(pdfGrid/2, r, colText()))
		}
	case "callout":
		c := col.New(pdfGrid).WithStyle(&props.Cell{BackgroundColor: rgbColor(Theme.Callout)})
		c.Add(text.New(b.Text, props.Text{Size: 11, Style: fontstyle.Italic, Color: rgbColor(Theme.Accent), Top: 2, Bottom: 4, Left: 4, Right: 4, VerticalPadding: 0.5}))
		m.AddAutoRow(c)
	case "code":
		// Monospaced on a tinted panel — for code samples / terminal output (the
		// model puts the snippet in the text field). maroto renders a literal "\n"
		// as a .notdef box and never breaks on it, so split into one tinted row per
		// line; preserveIndent keeps leading indentation through the space-based wrap.
		for _, ln := range strings.Split(b.Text, "\n") {
			ln = preserveIndent(ln)
			if ln == "" {
				ln = nbsp // keep blank lines visible (zero-height rows collapse)
			}
			c := col.New(pdfGrid).WithStyle(&props.Cell{BackgroundColor: rgbColor(Theme.CreamAlt)})
			c.Add(text.New(ln, props.Text{Size: 9, Family: pdfMonoFamily, Color: rgbColor(Theme.Ink), Left: 4, Right: 4, VerticalPadding: 0.4}))
			m.AddAutoRow(c)
		}
	default: // paragraph
		m.AddAutoRow(text.NewCol(pdfGrid, b.Text, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1, Bottom: 3, VerticalPadding: 0.5}))
	}
}
