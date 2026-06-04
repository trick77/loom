package docgen

import (
	"strings"

	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
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
		return nil
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

// sanitizeBlock applies sanitizeForPDF to every text field of a block.
func sanitizeBlock(b pdfBlock) pdfBlock {
	b.Text = sanitizeForPDF(b.Text)
	for i := range b.Items {
		b.Items[i] = sanitizeForPDF(b.Items[i])
	}
	for ri := range b.Rows {
		for ci := range b.Rows[ri] {
			b.Rows[ri][ci] = sanitizeForPDF(b.Rows[ri][ci])
		}
	}
	for i := range b.Left {
		b.Left[i] = sanitizeForPDF(b.Left[i])
	}
	for i := range b.Right {
		b.Right[i] = sanitizeForPDF(b.Right[i])
	}
	return b
}

// renderBlock appends maroto rows for one block.
func renderBlock(m core.Maroto, b pdfBlock) {
	switch b.Type {
	case "heading":
		size := 16.0
		if b.Level >= 2 {
			size = 13.0
		}
		m.AddRow(size*0.7+4, text.NewCol(12, b.Text, props.Text{Size: size, Style: fontstyle.Bold, Color: rgbColor(Theme.Accent), Top: 2}))
	case "bullets":
		for _, it := range b.Items {
			m.AddRow(6, text.NewCol(12, "•  "+it, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Left: 4, Top: 1}))
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
		span := 12 / cols
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
				cellStyle = &props.Cell{BackgroundColor: &props.Color{Red: 231, Green: 226, Blue: 214}}
			}
			cells := make([]core.Col, 0, cols)
			for ci := 0; ci < cols; ci++ {
				cell := ""
				if ci < len(r) {
					cell = r[ci]
				}
				cells = append(cells, text.NewCol(span, cell, props.Text{Size: 10, Style: style, Color: textColor, Top: 1, Left: 2}).WithStyle(cellStyle))
			}
			m.AddRow(8, cells...)
		}
	case "columns":
		m.AddRow(6,
			text.NewCol(6, strings.Join(b.Left, "\n"), props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1, Left: 2}),
			text.NewCol(6, strings.Join(b.Right, "\n"), props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1, Left: 2}),
		)
	case "callout":
		c := col.New(12).WithStyle(&props.Cell{BackgroundColor: &props.Color{Red: 244, Green: 238, Blue: 226}})
		c.Add(text.New(b.Text, props.Text{Size: 11, Style: fontstyle.Italic, Color: rgbColor(Theme.Accent), Top: 2, Left: 4}))
		m.AddRow(12, c)
	default: // paragraph
		m.AddRow(7, text.NewCol(12, b.Text, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1}))
	}
}
