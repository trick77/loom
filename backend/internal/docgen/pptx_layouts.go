package docgen

import (
	"fmt"
	"strings"
)

// EMU helpers: a 16:9 slide is 12192000 x 6858000 EMU.
const (
	slideWidthEMU  = 12192000
	slideHeightEMU = 6858000
)

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
