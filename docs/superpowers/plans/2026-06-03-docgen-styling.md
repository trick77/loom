# Document Generation Styling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Lume's generated PPTX and PDF documents look professional and varied by applying a shared Lume theme and adding layout variety plus tables.

**Architecture:** A new shared theme module supplies one palette to both generators. PPTX keeps its hand-written OOXML but gains styled per-layout renderers (background fills, accent titles, real bullets, tables). PDF is rebuilt on the `maroto/v2` grid library (accent header band, typed content blocks, automatic page breaks).

**Tech Stack:** Go, hand-written OOXML (PPTX), `github.com/johnfercher/maroto/v2` (PDF), Go fonts from `golang.org/x/image/font/gofont`.

**Spec:** `docs/superpowers/specs/2026-06-03-docgen-styling-design.md`

**Working dir:** worktree `../slopr-docgen-styling`, branch `feat/docgen-styling`. All `go` commands run from `backend/`.

---

## File Structure

- **Create** `backend/internal/docgen/theme.go` — shared palette (hex + RGB) and contrast helpers. No third-party imports.
- **Create** `backend/internal/docgen/theme_test.go`.
- **Create** `backend/internal/docgen/pptx_layouts.go` — OOXML shape helpers + one renderer per layout.
- **Create** `backend/internal/docgen/pptx_layouts_test.go`.
- **Modify** `backend/internal/docgen/pptx.go` — richer slide parsing, layout dispatch, background, schema.
- **Modify** `backend/internal/docgen/pptx_test.go` — keep existing assertions valid; add validity round-trip.
- **Rewrite** `backend/internal/docgen/pdf.go` — maroto-based generator with header band and typed blocks.
- **Modify** `backend/internal/docgen/pdf_test.go` — content fallback + blocks + multi-page tests.
- **Modify** `backend/go.mod` / `backend/go.sum` — add maroto.

`zipEntry(t, data, name) string` already exists in `docx_test.go` and is reused by PPTX tests.

---

## Task 1: Shared theme module

**Files:**
- Create: `backend/internal/docgen/theme.go`
- Test: `backend/internal/docgen/theme_test.go`

- [ ] **Step 1: Write the failing test**

```go
package docgen

import "testing"

func TestThemePaletteHexValues(t *testing.T) {
	if Theme.InkHex != "1D1D1B" || Theme.CreamHex != "F3F0E8" || Theme.AccentHex != "9A6B4F" {
		t.Fatalf("unexpected palette: %#v", Theme)
	}
}

func TestThemeRGBParsesHex(t *testing.T) {
	got := Theme.Accent
	if got.R != 0x9A || got.G != 0x6B || got.B != 0x4F {
		t.Fatalf("Accent RGB = %#v", got)
	}
}

func TestContrastTextOnAccentIsLight(t *testing.T) {
	// Dark accent → light text; light cream → dark text.
	if TextOn(Theme.Accent) != Theme.Cream {
		t.Fatalf("text on accent should be cream")
	}
	if TextOn(Theme.Cream) != Theme.Ink {
		t.Fatalf("text on cream should be ink")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestTheme -v`
Expected: FAIL — `undefined: Theme`.

- [ ] **Step 3: Write minimal implementation**

```go
package docgen

// RGB is an 8-bit-per-channel color, used by the PDF generator (maroto wants
// numeric channels) while the OOXML generators use the *Hex strings.
type RGB struct{ R, G, B int }

// palette holds the Lume brand colors once, so PPTX and PDF stay in sync.
type palette struct {
	Ink   RGB
	Cream RGB
	Accent RGB
	Sage  RGB
	Gold  RGB
	Muted RGB
	White RGB

	InkHex    string
	CreamHex  string
	AccentHex string
	SageHex   string
	GoldHex   string
	MutedHex  string
	WhiteHex  string
}

func hexToRGB(hex string) RGB {
	var r, g, b int
	_, _ = fmtSscanHex(hex, &r, &g, &b)
	return RGB{R: r, G: g, B: b}
}

// fmtSscanHex parses a 6-digit RRGGBB hex string into three ints.
func fmtSscanHex(hex string, r, g, b *int) (int, error) {
	parse := func(s string) int {
		n := 0
		for _, c := range s {
			n <<= 4
			switch {
			case c >= '0' && c <= '9':
				n |= int(c - '0')
			case c >= 'a' && c <= 'f':
				n |= int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				n |= int(c-'A') + 10
			}
		}
		return n
	}
	if len(hex) != 6 {
		return 0, nil
	}
	*r, *g, *b = parse(hex[0:2]), parse(hex[2:4]), parse(hex[4:6])
	return 3, nil
}

// Theme is the single shared Lume palette.
var Theme = func() palette {
	p := palette{
		InkHex: "1D1D1B", CreamHex: "F3F0E8", AccentHex: "9A6B4F",
		SageHex: "6F8B6B", GoldHex: "C7A35F", MutedHex: "5F5C54", WhiteHex: "FFFFFF",
	}
	p.Ink = hexToRGB(p.InkHex)
	p.Cream = hexToRGB(p.CreamHex)
	p.Accent = hexToRGB(p.AccentHex)
	p.Sage = hexToRGB(p.SageHex)
	p.Gold = hexToRGB(p.GoldHex)
	p.Muted = hexToRGB(p.MutedHex)
	p.White = hexToRGB(p.WhiteHex)
	return p
}()

// TextOn returns the legible text color for a given background, using a simple
// luminance threshold: light text on dark backgrounds, dark text on light ones.
func TextOn(bg RGB) RGB {
	luminance := (299*bg.R + 587*bg.G + 114*bg.B) / 1000
	if luminance < 140 {
		return Theme.Cream
	}
	return Theme.Ink
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run TestTheme -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/theme.go backend/internal/docgen/theme_test.go
git commit -m "feat(docgen): add shared Lume theme palette"
```

---

## Task 2: PPTX OOXML shape helpers

These small builders are reused by every layout. They live in `pptx_layouts.go`.

**Files:**
- Create: `backend/internal/docgen/pptx_layouts.go`
- Test: `backend/internal/docgen/pptx_layouts_test.go`

- [ ] **Step 1: Write the failing test**

```go
package docgen

import (
	"strings"
	"testing"
)

func TestSolidRectEmitsAccentFill(t *testing.T) {
	xml := solidRect(9, "Band", 0, 0, 12192000, 200000, Theme.AccentHex)
	if !strings.Contains(xml, `<a:srgbClr val="9A6B4F"/>`) || !strings.Contains(xml, `<a:off x="0" y="0"/>`) {
		t.Fatalf("solidRect = %s", xml)
	}
}

func TestStyledRunSetsColorAndBold(t *testing.T) {
	xml := styledRun("Hi", 4000, Theme.AccentHex, true, "ctr")
	if !strings.Contains(xml, `b="1"`) || !strings.Contains(xml, `algn="ctr"`) || !strings.Contains(xml, `<a:srgbClr val="9A6B4F"/>`) {
		t.Fatalf("styledRun = %s", xml)
	}
}

func TestBulletParaHasGlyph(t *testing.T) {
	xml := bulletPara("Point", 2000, Theme.InkHex)
	if !strings.Contains(xml, `<a:buChar char="`) || !strings.Contains(xml, "Point") {
		t.Fatalf("bulletPara = %s", xml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run "TestSolidRect|TestStyledRun|TestBulletPara" -v`
Expected: FAIL — `undefined: solidRect`.

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run "TestSolidRect|TestStyledRun|TestBulletPara" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx_layouts.go backend/internal/docgen/pptx_layouts_test.go
git commit -m "feat(docgen): add styled OOXML shape helpers for PPTX"
```

---

## Task 3: Richer slide model + layout dispatch

Extend the parsed slide with a `layout` and typed fields, and route rendering through a dispatcher. Default layout is `bullets` so existing payloads keep working.

**Files:**
- Modify: `backend/internal/docgen/pptx.go` (replace `pptxSlide`, `presentationSlides`, `pptxSlideXML`)
- Modify: `backend/internal/docgen/pptx_layouts.go` (add `renderSlide` dispatcher + `bulletsLayout`)
- Test: `backend/internal/docgen/pptx_layouts_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderSlideBulletsHasBackgroundAndAccentTitle(t *testing.T) {
	s := pptxSlide{Layout: "bullets", Title: "Phase 1", Bullets: []string{"Plan", "Build"}}
	xml := renderSlide(s)
	if !strings.Contains(xml, `<p:bg>`) {
		t.Fatalf("missing background: %s", xml)
	}
	if !strings.Contains(xml, `<a:srgbClr val="9A6B4F"/>`) { // accent title color
		t.Fatalf("missing accent title: %s", xml)
	}
	if !strings.Contains(xml, "Plan") || !strings.Contains(xml, "Build") {
		t.Fatalf("missing bullets: %s", xml)
	}
}

func TestRenderSlideUnknownLayoutFallsBackToBullets(t *testing.T) {
	s := pptxSlide{Layout: "nonsense", Title: "T", Bullets: []string{"x"}}
	xml := renderSlide(s)
	if !strings.Contains(xml, "x") || !strings.Contains(xml, `<p:bg>`) {
		t.Fatalf("fallback failed: %s", xml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestRenderSlide -v`
Expected: FAIL — `unknown field Layout` / `undefined: renderSlide`.

- [ ] **Step 3a: Replace the slide struct and parser in `pptx.go`**

Replace the `pptxSlide` type (lines ~14-17) with:

```go
type pptxSlide struct {
	Layout      string
	Title       string
	Subtitle    string
	Bullets     []string
	ColumnsLeft []string
	ColumnsRight []string
	Number      string
	Caption     string
	Quote       string
	Attribution string
	Table       [][]string
}
```

Replace `presentationSlides` so it reads the new optional fields (keep the existing title-required + bullet-string validation, add typed parsing):

```go
func presentationSlides(payload map[string]any) ([]pptxSlide, error) {
	rawSlides, ok := payload["slides"].([]any)
	if !ok {
		return nil, errors.New("slides are required")
	}
	slides := make([]pptxSlide, 0, len(rawSlides))
	for _, raw := range rawSlides {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("slides must contain objects")
		}
		title, ok := item["title"].(string)
		if !ok || strings.TrimSpace(title) == "" {
			return nil, errors.New("slide title is required")
		}
		slide := pptxSlide{
			Layout:      strings.TrimSpace(stringField(item, "layout")),
			Title:       strings.TrimSpace(title),
			Subtitle:    strings.TrimSpace(stringField(item, "subtitle")),
			Number:      strings.TrimSpace(stringField(item, "number")),
			Caption:     strings.TrimSpace(stringField(item, "caption")),
			Quote:       strings.TrimSpace(stringField(item, "quote")),
			Attribution: strings.TrimSpace(stringField(item, "attribution")),
		}
		bullets, err := stringList(item["bullets"], "slide bullets")
		if err != nil {
			return nil, err
		}
		slide.Bullets = bullets
		if cols, ok := item["columns"].(map[string]any); ok {
			if slide.ColumnsLeft, err = stringList(cols["left"], "column"); err != nil {
				return nil, err
			}
			if slide.ColumnsRight, err = stringList(cols["right"], "column"); err != nil {
				return nil, err
			}
		}
		slide.Table = stringMatrix(item["table"])
		slides = append(slides, slide)
	}
	return slides, nil
}

func stringField(item map[string]any, key string) string {
	v, _ := item[key].(string)
	return v
}

// stringList converts a JSON array of strings, skipping empties. nil input → nil slice.
func stringList(raw any, label string) ([]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New(label + " must be strings")
		}
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out, nil
}

// stringMatrix converts a JSON array of string-arrays (table rows). Malformed cells are skipped.
func stringMatrix(raw any) [][]string {
	rows, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out [][]string
	for _, r := range rows {
		cells, ok := r.([]any)
		if !ok {
			continue
		}
		row := make([]string, 0, len(cells))
		for _, c := range cells {
			s, _ := c.(string)
			row = append(row, strings.TrimSpace(s))
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}
```

Then change `pptxPackage`'s slide loop to call the dispatcher: replace `pptxSlideXML(slide)` with `renderSlide(slide)` and delete the old `pptxSlideXML` function.

- [ ] **Step 3b: Add the dispatcher + bullets layout in `pptx_layouts.go`**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/docgen/ -run "TestRenderSlide|TestPPTX" -v`
Expected: PASS, including the pre-existing `TestPPTXGeneratorWritesPresentationPackage` (slide still contains "Phase 1"/"Build").

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx.go backend/internal/docgen/pptx_layouts.go backend/internal/docgen/pptx_layouts_test.go
git commit -m "feat(docgen): styled PPTX bullets layout + layout dispatch"
```

---

## Task 4: PPTX `title` and `section` layouts

**Files:**
- Modify: `backend/internal/docgen/pptx_layouts.go`
- Test: `backend/internal/docgen/pptx_layouts_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestTitleLayoutCentersTitleAndSubtitle(t *testing.T) {
	xml := titleLayout(pptxSlide{Layout: "title", Title: "Lume", Subtitle: "Q3 Review"})
	if !strings.Contains(xml, "Lume") || !strings.Contains(xml, "Q3 Review") || !strings.Contains(xml, `algn="ctr"`) {
		t.Fatalf("titleLayout = %s", xml)
	}
}

func TestSectionLayoutUsesAccentBackground(t *testing.T) {
	xml := sectionLayout(pptxSlide{Layout: "section", Title: "Part II"})
	if !strings.Contains(xml, `<a:srgbClr val="9A6B4F"/>`) || !strings.Contains(xml, "Part II") {
		t.Fatalf("sectionLayout = %s", xml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run "TestTitleLayout|TestSectionLayout" -v`
Expected: FAIL — `undefined: titleLayout`.

- [ ] **Step 3: Implement**

```go
// titleLayout: large centered title with an accent band behind it and a muted subtitle.
func titleLayout(s pptxSlide) string {
	band := solidRect(2, "Title Band", 0, 2300000, slideWidthEMU, 1500000, Theme.AccentHex)
	title := textBox(3, "Title", 685800, 2450000, 10820400, 1000000,
		styledRun(s.Title, 5400, Theme.WhiteHex, true, "ctr"))
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
		styledRun(s.Title, 4800, Theme.CreamHex, true, "ctr"))
	return slideEnvelope(Theme.AccentHex, title)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run "TestTitleLayout|TestSectionLayout" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx_layouts.go backend/internal/docgen/pptx_layouts_test.go
git commit -m "feat(docgen): PPTX title and section layouts"
```

---

## Task 5: PPTX `two-column`, `quote`, `big-number` layouts

**Files:**
- Modify: `backend/internal/docgen/pptx_layouts.go`
- Test: `backend/internal/docgen/pptx_layouts_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestTwoColumnLayoutRendersBothColumns(t *testing.T) {
	xml := twoColumnLayout(pptxSlide{Title: "Compare", ColumnsLeft: []string{"A"}, ColumnsRight: []string{"B"}})
	if !strings.Contains(xml, "A") || !strings.Contains(xml, "B") || !strings.Contains(xml, "Compare") {
		t.Fatalf("twoColumnLayout = %s", xml)
	}
}

func TestQuoteLayoutShowsQuoteAndAttribution(t *testing.T) {
	xml := quoteLayout(pptxSlide{Quote: "Ship it", Attribution: "Jan"})
	if !strings.Contains(xml, "Ship it") || !strings.Contains(xml, "Jan") {
		t.Fatalf("quoteLayout = %s", xml)
	}
}

func TestBigNumberLayoutShowsNumberAndCaption(t *testing.T) {
	xml := bigNumberLayout(pptxSlide{Title: "Growth", Number: "85%", Caption: "YoY"})
	if !strings.Contains(xml, "85%") || !strings.Contains(xml, "YoY") {
		t.Fatalf("bigNumberLayout = %s", xml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run "TestTwoColumn|TestQuoteLayout|TestBigNumber" -v`
Expected: FAIL — `undefined: twoColumnLayout`.

- [ ] **Step 3: Implement**

```go
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

// quoteLayout: a gold accent bar, a large italic-feel quote, and an attribution line.
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run "TestTwoColumn|TestQuoteLayout|TestBigNumber" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx_layouts.go backend/internal/docgen/pptx_layouts_test.go
git commit -m "feat(docgen): PPTX two-column, quote, big-number layouts"
```

---

## Task 6: PPTX `table` layout (DrawingML table)

**Files:**
- Modify: `backend/internal/docgen/pptx_layouts.go`
- Test: `backend/internal/docgen/pptx_layouts_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestTableLayoutEmitsTableWithAccentHeader(t *testing.T) {
	xml := tableLayout(pptxSlide{Title: "Data", Table: [][]string{{"Name", "Value"}, {"A", "1"}}})
	if !strings.Contains(xml, "<a:tbl>") || !strings.Contains(xml, "Name") || !strings.Contains(xml, "A") {
		t.Fatalf("tableLayout = %s", xml)
	}
	if !strings.Contains(xml, `<a:srgbClr val="9A6B4F"/>`) { // accent header fill
		t.Fatalf("missing accent header fill: %s", xml)
	}
}

func TestTableLayoutWithoutRowsFallsBackToTitleOnly(t *testing.T) {
	xml := tableLayout(pptxSlide{Title: "Empty"})
	if strings.Contains(xml, "<a:tbl>") || !strings.Contains(xml, "Empty") {
		t.Fatalf("expected no table, got %s", xml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestTableLayout -v`
Expected: FAIL — `undefined: tableLayout`.

- [ ] **Step 3: Implement**

```go
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
			textHex = Theme.WhiteHex
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run TestTableLayout -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx_layouts.go backend/internal/docgen/pptx_layouts_test.go
git commit -m "feat(docgen): PPTX table layout"
```

---

## Task 7: PPTX schema + tool description

Expose the new fields so the model can use them.

**Files:**
- Modify: `backend/internal/docgen/pptx.go` (`Schema` method)
- Test: `backend/internal/docgen/pptx_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestPPTXSchema -v`
Expected: FAIL — layout key missing.

- [ ] **Step 3: Implement** — replace the `Schema` method body's slide item properties and description:

```go
func (g PPTXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create a styled PPTX presentation. Open with a 'title' slide, use 'section' " +
			"dividers between parts, choose 'table' for tabular data, 'two-column' to compare, " +
			"'big-number' to highlight a stat, and 'quote' for a pull quote. Vary layouts; do not " +
			"put everything in plain bullets.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"title":    map[string]any{"type": "string"},
				"slides": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"layout": map[string]any{
								"type":        "string",
								"enum":        []string{"title", "section", "bullets", "two-column", "big-number", "quote", "table"},
								"description": "Slide layout. Defaults to bullets.",
							},
							"title":       map[string]any{"type": "string"},
							"subtitle":    map[string]any{"type": "string"},
							"bullets":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"columns": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"left":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									"right": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								},
							},
							"number":      map[string]any{"type": "string"},
							"caption":     map[string]any{"type": "string"},
							"quote":       map[string]any{"type": "string"},
							"attribution": map[string]any{"type": "string"},
							"table": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							},
						},
						"required": []string{"title"},
					},
				},
			},
			"required":             []string{"filename", "slides"},
			"additionalProperties": false,
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run "TestPPTX" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pptx.go backend/internal/docgen/pptx_test.go
git commit -m "feat(docgen): expose PPTX layout fields in tool schema"
```

---

## Task 8: PPTX validity round-trip (LibreOffice)

Guards against an invalid OOXML part triggering PowerPoint's repair prompt. The test is skipped when `soffice` is absent so CI without LibreOffice stays green; run it locally where `soffice` exists.

**Files:**
- Test: `backend/internal/docgen/pptx_test.go`

- [ ] **Step 1: Write the test**

```go
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
```

Add imports to `pptx_test.go`: `os`, `os/exec`, `path/filepath`.

- [ ] **Step 2: Run the test**

Run: `cd backend && go test ./internal/docgen/ -run TestPPTXAllLayoutsConvert -v`
Expected: PASS locally (soffice present) — `all.pdf` is produced with no repair error. If `soffice` is missing it SKIPs.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/docgen/pptx_test.go
git commit -m "test(docgen): LibreOffice round-trip validates all PPTX layouts"
```

---

## Task 9: Add maroto dependency

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`

- [ ] **Step 1: Add the module**

Run: `cd backend && go get github.com/johnfercher/maroto/v2@latest`
Expected: `go.mod` gains `github.com/johnfercher/maroto/v2`, `go.sum` updated.

- [ ] **Step 2: Verify it builds**

Run: `cd backend && go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "build(docgen): add maroto/v2 for styled PDF generation"
```

---

## Task 10: PDF rebuilt on maroto — fonts, header band, content fallback

Rewrite `pdf.go`. This task keeps the existing `content`-string behavior working (so `pdf_test.go`'s existing tests pass) while routing it through maroto with a styled header band.

**Files:**
- Rewrite: `backend/internal/docgen/pdf.go`
- Test: `backend/internal/docgen/pdf_test.go` (existing tests must still pass)

- [ ] **Step 1: Run existing tests to capture the baseline**

Run: `cd backend && go test ./internal/docgen/ -run TestPDF -v`
Expected: existing PASS (current gopdf impl). These must stay green after the rewrite.

- [ ] **Step 2: Rewrite `pdf.go`**

```go
package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/fontrepository"
	"github.com/johnfercher/maroto/v2/pkg/props"
	"github.com/trick77/lume/internal/artifact"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

const pdfFontFamily = "slopr"

type PDFGenerator struct{}

func (g PDFGenerator) ToolName() string { return "create_pdf_file" }

func rgbColor(c RGB) *props.Color { return &props.Color{Red: c.R, Green: c.G, Blue: c.B} }

// newMaroto builds a maroto instance with the Lume fonts and an accent header band.
func newMaroto(title, subtitle string) (core.Maroto, error) {
	fonts, err := fontrepository.New().
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Normal, goregular.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Bold, gobold.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Italic, goitalic.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.BoldItalic, gobolditalic.TTF).
		Load()
	if err != nil {
		return nil, err
	}
	cfg := config.NewBuilder().
		WithCustomFonts(fonts).
		WithDefaultFont(&props.Font{Family: pdfFontFamily}).
		Build()
	m := maroto.New(cfg)

	band := col.New(12).WithStyle(&props.Cell{BackgroundColor: rgbColor(Theme.Accent)})
	band.Add(text.New(title, props.Text{Size: 22, Style: fontstyle.Bold, Color: rgbColor(Theme.White), Align: align.Left, Top: 4, Left: 4}))
	m.AddRow(20, band)
	if strings.TrimSpace(subtitle) != "" {
		m.AddRow(8, text.NewCol(12, subtitle, props.Text{Size: 11, Color: rgbColor(Theme.Muted), Top: 1}))
	}
	m.AddRow(6, text.NewCol(12, "")) // spacer
	return m, nil
}

func (g PDFGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	title, _ := req.Payload["title"].(string)
	blocks := parseBlocks(req.Payload)
	content, _ := req.Payload["content"].(string)
	if len(blocks) == 0 && strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content or blocks are required")
	}
	if len(blocks) == 0 {
		blocks = blocksFromMarkdown(content)
	}
	subtitle, _ := req.Payload["subtitle"].(string)
	if strings.TrimSpace(title) == "" {
		title = "Document"
	}
	m, err := newMaroto(title, subtitle)
	if err != nil {
		return GeneratedMeta{}, err
	}
	for _, b := range blocks {
		renderBlock(m, b)
	}
	doc, err := m.Generate()
	if err != nil {
		return GeneratedMeta{}, err
	}
	if _, err := w.Write(doc.GetBytes()); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pdf", MIMEType: artifact.MIMEType("pdf")}, nil
}

// row.New / col.New imported via maroto components; renderBlock + parseBlocks defined in pdf_blocks.go (Task 11/12).
var _ = row.New // keep the row import referenced until blocks are added
```

> Note: `parseBlocks`, `blocksFromMarkdown`, and `renderBlock` are introduced in Tasks 11–12. To keep this task compiling on its own, also add the stub file below in this same task, then flesh it out next.

- [ ] **Step 3: Add a minimal `pdf_blocks.go` so the package compiles**

Create `backend/internal/docgen/pdf_blocks.go`:

```go
package docgen

import (
	"strings"

	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

type pdfBlock struct {
	Type    string // heading | paragraph | bullets | table | columns | callout
	Level   int
	Text    string
	Items   []string
	Rows    [][]string
	Left    []string
	Right   []string
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

// renderBlock appends maroto rows for one block. Only paragraph/heading handled here;
// bullets/table/columns/callout are added in Task 12.
func renderBlock(m core.Maroto, b pdfBlock) {
	switch b.Type {
	case "heading":
		size := 16.0
		if b.Level >= 2 {
			size = 13.0
		}
		m.AddRow(size*0.7+4, text.NewCol(12, b.Text, props.Text{Size: size, Style: fontstyle.Bold, Color: rgbColor(Theme.Accent), Top: 2}))
	default:
		m.AddRow(7, text.NewCol(12, b.Text, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1}))
	}
}
```

Remove the temporary `var _ = row.New` line from `pdf.go` once `pdf_blocks.go` uses the imports; adjust `pdf.go` imports so only used packages remain (`go build` will tell you).

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/docgen/ -run TestPDF -v`
Expected: existing `TestPDFGeneratorWritesPDF` (it passes `content` + `title`) still PASS — output starts with `%PDF-`. `TestPDFGeneratorRejectsEmptyContent` still PASS (empty content + no blocks → error).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pdf.go backend/internal/docgen/pdf_blocks.go
git commit -m "feat(docgen): rebuild PDF generator on maroto with themed header"
```

---

## Task 11: PDF heading/paragraph/bullets blocks

**Files:**
- Modify: `backend/internal/docgen/pdf_blocks.go`
- Test: `backend/internal/docgen/pdf_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPDFRendersTypedBlocks(t *testing.T) {
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "r.pdf",
		Payload: map[string]any{
			"title": "Report",
			"blocks": []any{
				map[string]any{"type": "heading", "level": float64(1), "text": "Summary"},
				map[string]any{"type": "paragraph", "text": "All good."},
				map[string]any{"type": "bullets", "items": []any{"one", "two", "three"}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", out.Bytes()[:8])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestPDFRendersTypedBlocks -v`
Expected: it likely PASSES already for heading/paragraph but bullets render as nothing. To make the test meaningful, assert bullets are handled — extend `renderBlock` so bullets are not dropped. Proceed to Step 3 and confirm no panic and a valid PDF.

- [ ] **Step 3: Implement bullets in `renderBlock`** — add a case before `default`:

```go
	case "bullets":
		for _, it := range b.Items {
			m.AddRow(6, text.NewCol(12, "•  "+it, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Left: 4, Top: 1}))
		}
	case "paragraph":
		m.AddRow(7, text.NewCol(12, b.Text, props.Text{Size: 11, Color: rgbColor(Theme.Ink), Top: 1}))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run TestPDFRendersTypedBlocks -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pdf_blocks.go backend/internal/docgen/pdf_test.go
git commit -m "feat(docgen): PDF heading/paragraph/bullets blocks"
```

---

## Task 12: PDF table, columns, callout blocks

**Files:**
- Modify: `backend/internal/docgen/pdf_blocks.go`
- Test: `backend/internal/docgen/pdf_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPDFRendersTableColumnsCallout(t *testing.T) {
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "r.pdf",
		Payload: map[string]any{
			"title": "Report",
			"blocks": []any{
				map[string]any{"type": "table", "rows": []any{[]any{"Name", "Value"}, []any{"A", "1"}, []any{"B", "2"}}},
				map[string]any{"type": "columns", "left": []any{"L1"}, "right": []any{"R1"}},
				map[string]any{"type": "callout", "text": "Important note"},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestPDFRendersTableColumnsCallout -v`
Expected: PASS for the build but table/columns/callout render nothing (they hit `default`). Implement to render them properly.

- [ ] **Step 3: Implement the three cases in `renderBlock`** (add before `default`):

```go
	case "table":
		for ri, r := range b.Rows {
			header := ri == 0
			cellStyle := &props.Cell{BackgroundColor: rgbColor(Theme.Cream)}
			textColor := rgbColor(Theme.Ink)
			style := fontstyle.Normal
			if header {
				cellStyle = &props.Cell{BackgroundColor: rgbColor(Theme.Accent)}
				textColor = rgbColor(Theme.White)
				style = fontstyle.Bold
			} else if ri%2 == 1 {
				cellStyle = &props.Cell{BackgroundColor: &props.Color{Red: 231, Green: 226, Blue: 214}}
			}
			cols := make([]core.Col, 0, len(r))
			span := 12
			if len(r) > 0 {
				span = 12 / len(r)
				if span == 0 {
					span = 1
				}
			}
			for _, cell := range r {
				cols = append(cols, text.NewCol(span, cell, props.Text{Size: 10, Style: style, Color: textColor, Top: 1, Left: 2}).WithStyle(cellStyle))
			}
			m.AddRow(8, cols...)
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
```

Add `"github.com/johnfercher/maroto/v2/pkg/components/col"` to `pdf_blocks.go` imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run TestPDF -v`
Expected: PASS (all PDF tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pdf_blocks.go backend/internal/docgen/pdf_test.go
git commit -m "feat(docgen): PDF table, columns, and callout blocks"
```

---

## Task 13: PDF schema + tool description

**Files:**
- Modify: `backend/internal/docgen/pdf.go` (`Schema` method)
- Test: `backend/internal/docgen/pdf_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPDFSchemaAdvertisesBlocks(t *testing.T) {
	schema := PDFGenerator{}.Schema()
	props, _ := schema.Parameters["properties"].(map[string]any)
	if _, ok := props["blocks"]; !ok {
		t.Fatalf("schema missing blocks: %#v", props)
	}
	if _, ok := props["content"]; !ok {
		t.Fatalf("schema should keep content fallback")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/docgen/ -run TestPDFSchema -v`
Expected: FAIL — `blocks` missing.

- [ ] **Step 3: Implement** — add the `Schema` method to `pdf.go`:

```go
func (g PDFGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create a styled PDF report. Prefer the structured 'blocks' array (heading, " +
			"paragraph, bullets, table, columns, callout) over a flat text string: use headings to " +
			"structure sections, tables for tabular data, and callouts to emphasize key points. " +
			"'content' is accepted as a simple Markdown fallback.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"title":    map[string]any{"type": "string"},
				"subtitle": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string", "description": "Markdown fallback when blocks is omitted."},
				"blocks": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":  map[string]any{"type": "string", "enum": []string{"heading", "paragraph", "bullets", "table", "columns", "callout"}},
							"level": map[string]any{"type": "integer"},
							"text":  map[string]any{"type": "string"},
							"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"rows":  map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
							"left":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"right": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"type"},
					},
				},
			},
			"required":             []string{"filename"},
			"additionalProperties": false,
		},
	}
}
```

Note: `filename` is now the only hard requirement (either `blocks` or `content` supplies the body; `Generate` already errors if both are empty).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/docgen/ -run TestPDF -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/docgen/pdf.go backend/internal/docgen/pdf_test.go
git commit -m "feat(docgen): expose PDF blocks in tool schema"
```

---

## Task 14: PDF multi-page (overflow) regression test

Confirms the old gopdf overflow bug is gone — long content must span multiple pages instead of clipping.

**Files:**
- Test: `backend/internal/docgen/pdf_test.go`

- [ ] **Step 1: Write the test**

```go
func TestPDFLongContentSpansMultiplePages(t *testing.T) {
	blocks := make([]any, 0, 120)
	for i := 0; i < 120; i++ {
		blocks = append(blocks, map[string]any{"type": "paragraph", "text": "Line of body content that fills vertical space."})
	}
	var out bytes.Buffer
	_, err := PDFGenerator{}.Generate(GenerateRequest{
		Filename: "long.pdf",
		Payload:  map[string]any{"title": "Long", "blocks": blocks},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// A multi-page PDF declares more than one /Page object.
	if n := bytes.Count(out.Bytes(), []byte("/Type /Page")); n < 2 {
		if n2 := bytes.Count(out.Bytes(), []byte("/Type/Page")); n2 < 2 {
			t.Fatalf("expected multiple pages, found markers %d/%d", n, n2)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd backend && go test ./internal/docgen/ -run TestPDFLongContent -v`
Expected: PASS — maroto auto-paginates. (If the page marker string differs in maroto's output, adjust the matched literal to whichever of `/Type /Page` or `/Type/Page` the bytes contain; verify by printing once.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/docgen/pdf_test.go
git commit -m "test(docgen): PDF paginates long content across pages"
```

---

## Task 15: Full suite + manual visual verification

**Files:** none (verification only), plus a throwaway script you delete.

- [ ] **Step 1: Run the whole backend suite**

Run: `cd backend && go test ./...`
Expected: all PASS.

- [ ] **Step 2: Vet and build**

Run: `cd backend && go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 3: Generate real sample files and eyeball them**

Write a temporary `backend/cmd/docgensample/main.go` that calls `PPTXGenerator.Generate` and `PDFGenerator.Generate` with a payload exercising every layout/block (reuse the payloads from Tasks 8 and 12), writing `sample.pptx` and `sample.pdf` to a temp dir. Run it, then:

Run: `soffice --headless --convert-to pdf --outdir /tmp/docgen /tmp/docgen/sample.pptx`
Expected: clean conversion (no "repair" message in output), and open `sample.pptx` / `sample.pdf` to confirm colors, accent bars, real bullets, tables, and the PDF header band all render.

- [ ] **Step 4: Delete the throwaway sample command**

```bash
rm -rf backend/cmd/docgensample
```

- [ ] **Step 5: Final commit (if anything outstanding) and push**

```bash
git status   # expect clean other than untracked nothing
git push -u origin feat/docgen-styling
```

---

## Self-Review notes

- **Spec coverage:** theme layer (Task 1) ✓; PPTX 7 layouts (Tasks 3–6) ✓; PPTX schema/description (Task 7) ✓; PPTX validity (Task 8) ✓; maroto PDF with header band + blocks + content fallback (Tasks 9–13) ✓; PDF page-break fix (Task 14) ✓; visual verification (Task 15) ✓.
- **Deviation from spec to flag to the user:** the spec named Go fonts for PDF; the plan keeps that (registers goregular/gobold/goitalic/gobolditalic from embedded bytes via `AddUTF8FontFromBytes`) — no deviation. PPTX keeps the existing `Aptos` theme fonts.
- **Type consistency:** `pptxSlide` fields (`Layout`, `Bullets`, `ColumnsLeft/Right`, `Number`, `Caption`, `Quote`, `Attribution`, `Table`) used consistently across Tasks 3–6; helper signatures (`solidRect`, `styledRun`, `bulletPara`, `textBox`, `slideEnvelope`, `renderSlide`) stable; PDF `pdfBlock` fields and `renderBlock`/`parseBlocks`/`blocksFromMarkdown` consistent across Tasks 10–13.
- **Known soft spot:** maroto's exact page-object marker bytes (Task 14) and any unused-import churn during the Task 10 rewrite are resolved empirically with `go build`/printing, as noted inline.
