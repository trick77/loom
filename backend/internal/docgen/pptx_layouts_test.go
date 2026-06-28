package docgen

import (
	"strings"
	"testing"
)

func TestSolidRectEmitsAccentFill(t *testing.T) {
	xml := solidRect(9, "Band", 0, 0, 12192000, 200000, Theme.AccentHex)
	if !strings.Contains(xml, `<a:srgbClr val="C15F3C"/>`) || !strings.Contains(xml, `<a:off x="0" y="0"/>`) {
		t.Fatalf("solidRect = %s", xml)
	}
}

func TestStyledRunSetsColorAndBold(t *testing.T) {
	xml := styledRun("Hi", 4000, Theme.AccentHex, true, "ctr")
	if !strings.Contains(xml, `b="1"`) || !strings.Contains(xml, `algn="ctr"`) || !strings.Contains(xml, `<a:srgbClr val="C15F3C"/>`) {
		t.Fatalf("styledRun = %s", xml)
	}
}

func TestBulletParaHasGlyph(t *testing.T) {
	xml := bulletPara("Point", 2000, Theme.InkHex)
	if !strings.Contains(xml, `<a:buChar char="`) || !strings.Contains(xml, "Point") {
		t.Fatalf("bulletPara = %s", xml)
	}
}

func TestRenderSlideBulletsHasBackgroundAndAccentTitle(t *testing.T) {
	s := pptxSlide{Layout: "bullets", Title: "Phase 1", Bullets: []string{"Plan", "Build"}}
	xml := renderSlide(s)
	if !strings.Contains(xml, `<p:bg>`) {
		t.Fatalf("missing background: %s", xml)
	}
	if !strings.Contains(xml, `<a:srgbClr val="C15F3C"/>`) { // accent title color
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

func TestTitleLayoutCentersTitleAndSubtitle(t *testing.T) {
	xml := titleLayout(pptxSlide{Layout: "title", Title: "Lume", Subtitle: "Q3 Review"})
	if !strings.Contains(xml, "Lume") || !strings.Contains(xml, "Q3 Review") || !strings.Contains(xml, `algn="ctr"`) {
		t.Fatalf("titleLayout = %s", xml)
	}
}

func TestSectionLayoutUsesAccentBackground(t *testing.T) {
	xml := sectionLayout(pptxSlide{Layout: "section", Title: "Part II"})
	if !strings.Contains(xml, `<a:srgbClr val="C15F3C"/>`) || !strings.Contains(xml, "Part II") {
		t.Fatalf("sectionLayout = %s", xml)
	}
}

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

func TestTableLayoutEmitsTableWithAccentHeader(t *testing.T) {
	xml := tableLayout(pptxSlide{Title: "Data", Table: [][]string{{"Name", "Value"}, {"A", "1"}}})
	if !strings.Contains(xml, "<a:tbl>") || !strings.Contains(xml, "Name") || !strings.Contains(xml, "A") {
		t.Fatalf("tableLayout = %s", xml)
	}
	if !strings.Contains(xml, `<a:srgbClr val="C15F3C"/>`) { // accent header fill
		t.Fatalf("missing accent header fill: %s", xml)
	}
}

func TestTableLayoutWithoutRowsFallsBackToTitleOnly(t *testing.T) {
	xml := tableLayout(pptxSlide{Title: "Empty"})
	if strings.Contains(xml, "<a:tbl>") || !strings.Contains(xml, "Empty") {
		t.Fatalf("expected no table, got %s", xml)
	}
}
