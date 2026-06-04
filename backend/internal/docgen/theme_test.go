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
