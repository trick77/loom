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
