package docgen

import (
	"fmt"
	"strings"
)

// EMU helper: a 16:9 slide is 12192000 EMU wide.
const slideWidthEMU = 12192000

// solidRect renders a filled rectangle shape (used for backgrounds and accent bars).
func solidRect(id int, name string, x, y, cx, cy int, hex string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`+
		`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>`+
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>`, id, xmlText(name), x, y, cx, cy, hex)
}

// styledRun renders one text run wrapped in a paragraph with color, size, bold and alignment.
// align is an OOXML algn value: "l", "ctr", or "r".
func styledRun(text string, sz int, hex string, bold bool, align string) string {
	b := "0"
	if bold {
		b = "1"
	}
	return fmt.Sprintf(`<a:p><a:pPr algn="%s"/><a:r><a:rPr lang="en-US" sz="%d" b="%s"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		align, sz, b, hex, xmlText(text))
}

// bulletPara renders one bulleted paragraph with a real bullet glyph in the accent color.
func bulletPara(text string, sz int, hex string) string {
	return fmt.Sprintf(`<a:p><a:pPr marL="285750" indent="-285750"><a:buClr><a:srgbClr val="%s"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="%s"/></a:pPr>`+
		`<a:r><a:rPr lang="en-US" sz="%d"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		Theme.AccentHex, "•", sz, hex, xmlText(text))
}

// textBox renders a text-body shape at the given EMU rectangle, containing pre-rendered paragraphs.
func textBox(id int, name string, x, y, cx, cy int, paragraphs string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr/></p:nvSpPr>`+
		`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm></p:spPr>`+
		`<p:txBody><a:bodyPr wrap="square"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		id, xmlText(name), x, y, cx, cy, paragraphs)
}

// joinParas concatenates rendered paragraph fragments.
func joinParas(parts []string) string { return strings.Join(parts, "") }

// slideEnvelope wraps a background fill and body shapes into a full slide part.
func slideEnvelope(bgHex string, body string) string {
	bg := fmt.Sprintf(`<p:bg><p:bgPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`, bgHex)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>` + bg + `<p:spTree>
    <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		body + `
  </p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>
</p:sld>`
}

// renderSlide dispatches to the layout renderer. Unknown layouts fall back to bullets.
func renderSlide(s pptxSlide) string {
	switch s.Layout {
	case "title":
		return titleLayout(s)
	case "section":
		return sectionLayout(s)
	case "two-column":
		return twoColumnLayout(s)
	case "big-number":
		return bigNumberLayout(s)
	case "quote":
		return quoteLayout(s)
	case "table":
		return tableLayout(s)
	default:
		return bulletsLayout(s)
	}
}

// bulletsLayout: accent title + accent bar + a single body box of real bullets, on cream.
func bulletsLayout(s pptxSlide) string {
	title := textBox(2, "Title", 685800, 457200, 10820400, 1000000,
		styledRun(s.Title, 3600, Theme.AccentHex, true, "l"))
	bar := solidRect(3, "Accent Bar", 685800, 1490000, 2400000, 60000, Theme.GoldHex)
	var paras []string
	for _, b := range s.Bullets {
		paras = append(paras, bulletPara(b, 2000, Theme.InkHex))
	}
	body := textBox(4, "Body", 685800, 1750000, 10820400, 4400000, joinParas(paras))
	return slideEnvelope(Theme.CreamHex, title+bar+body)
}

// titleLayout: large centered title with an accent band behind it and a muted subtitle.
func titleLayout(s pptxSlide) string {
	band := solidRect(2, "Title Band", 0, 2300000, slideWidthEMU, 1500000, Theme.AccentHex)
	title := textBox(3, "Title", 685800, 2450000, 10820400, 1000000,
		styledRun(s.Title, 5400, textOnHex(Theme.AccentHex), true, "ctr"))
	body := band + title
	if s.Subtitle != "" {
		body += textBox(4, "Subtitle", 685800, 4000000, 10820400, 700000,
			styledRun(s.Subtitle, 2400, Theme.MutedHex, false, "ctr"))
	}
	return slideEnvelope(Theme.CreamHex, body)
}

// sectionLayout: full-bleed accent background with a large light, centered title.
func sectionLayout(s pptxSlide) string {
	title := textBox(2, "Section Title", 685800, 2900000, 10820400, 1100000,
		styledRun(s.Title, 4800, textOnHex(Theme.AccentHex), true, "ctr"))
	return slideEnvelope(Theme.AccentHex, title)
}

// twoColumnLayout: accent title + two side-by-side bullet boxes.
func twoColumnLayout(s pptxSlide) string {
	title := textBox(2, "Title", 685800, 457200, 10820400, 1000000,
		styledRun(s.Title, 3600, Theme.AccentHex, true, "l"))
	left := textBox(3, "Left", 685800, 1750000, 5200000, 4400000, columnParas(s.ColumnsLeft))
	right := textBox(4, "Right", 6306600, 1750000, 5200000, 4400000, columnParas(s.ColumnsRight))
	return slideEnvelope(Theme.CreamHex, title+left+right)
}

func columnParas(items []string) string {
	var paras []string
	for _, it := range items {
		paras = append(paras, bulletPara(it, 1800, Theme.InkHex))
	}
	return joinParas(paras)
}

// quoteLayout: a gold accent bar, a large quote, and an attribution line.
func quoteLayout(s pptxSlide) string {
	bar := solidRect(2, "Quote Bar", 685800, 2300000, 120000, 1600000, Theme.GoldHex)
	quote := textBox(3, "Quote", 1100000, 2300000, 10000000, 1700000,
		styledRun("“"+s.Quote+"”", 3200, Theme.InkHex, true, "l"))
	body := bar + quote
	if s.Attribution != "" {
		body += textBox(4, "Attribution", 1100000, 4100000, 10000000, 600000,
			styledRun("— "+s.Attribution, 2000, Theme.MutedHex, false, "l"))
	}
	return slideEnvelope(Theme.CreamHex, body)
}

// bigNumberLayout: a huge accent-colored figure with a caption and optional title.
func bigNumberLayout(s pptxSlide) string {
	var body string
	if s.Title != "" {
		body += textBox(2, "Title", 685800, 700000, 10820400, 800000,
			styledRun(s.Title, 2800, Theme.MutedHex, false, "ctr"))
	}
	body += textBox(3, "Number", 685800, 2200000, 10820400, 1800000,
		styledRun(s.Number, 9600, Theme.AccentHex, true, "ctr"))
	if s.Caption != "" {
		body += textBox(4, "Caption", 685800, 4300000, 10820400, 800000,
			styledRun(s.Caption, 2400, Theme.InkHex, false, "ctr"))
	}
	return slideEnvelope(Theme.CreamHex, body)
}

// tableLayout: accent title + a DrawingML table (header row in accent, zebra body rows).
func tableLayout(s pptxSlide) string {
	title := textBox(2, "Title", 685800, 457200, 10820400, 900000,
		styledRun(s.Title, 3600, Theme.AccentHex, true, "l"))
	if len(s.Table) == 0 {
		return slideEnvelope(Theme.CreamHex, title)
	}
	return slideEnvelope(Theme.CreamHex, title+tableFrame(3, s.Table))
}

// tableFrame builds a graphicFrame wrapping an a:tbl. Column widths split the usable width evenly.
func tableFrame(id int, rows [][]string) string {
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return ""
	}
	totalW := 10820400
	colW := totalW / cols
	var grid strings.Builder
	for i := 0; i < cols; i++ {
		grid.WriteString(fmt.Sprintf(`<a:gridCol w="%d"/>`, colW))
	}
	var body strings.Builder
	for ri, r := range rows {
		header := ri == 0
		fill := Theme.CreamHex
		textHex := Theme.InkHex
		if header {
			fill = Theme.AccentHex
			textHex = textOnHex(Theme.AccentHex)
		} else if ri%2 == 0 {
			fill = "E7E2D6" // subtle zebra band
		}
		body.WriteString(`<a:tr h="370840">`)
		for ci := 0; ci < cols; ci++ {
			cell := ""
			if ci < len(r) {
				cell = r[ci]
			}
			body.WriteString(fmt.Sprintf(`<a:tc><a:txBody><a:bodyPr/><a:lstStyle/>%s</a:txBody>`+
				`<a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`,
				styledRun(cell, 1600, textHex, header, "l"), fill))
		}
		body.WriteString(`</a:tr>`)
	}
	return fmt.Sprintf(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>`+
		`<p:xfrm><a:off x="685800" y="1600000"/><a:ext cx="%d" cy="%d"/></p:xfrm>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">`+
		`<a:tbl><a:tblPr firstRow="1"><a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId></a:tblPr>`+
		`<a:tblGrid>%s</a:tblGrid>%s</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`,
		id, totalW, len(rows)*370840, grid.String(), body.String())
}
