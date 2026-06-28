package docgen

import "fmt"

// RGB is an 8-bit-per-channel color, used by the PDF generator (maroto wants
// numeric channels) while the OOXML generators use the *Hex strings.
type RGB struct{ R, G, B int }

// palette holds the Loom brand colors once, so PPTX and PDF stay in sync.
type palette struct {
	Ink      RGB
	Cream    RGB
	CreamAlt RGB // slightly darker Cream, used for PDF zebra table rows
	Accent   RGB
	Sage     RGB
	Gold     RGB
	Muted    RGB
	White    RGB
	Callout  RGB // warm tint behind PDF callout blocks

	InkHex      string
	CreamHex    string
	CreamAltHex string
	AccentHex   string
	SageHex     string
	GoldHex     string
	MutedHex    string
	WhiteHex    string
	CalloutHex  string
}

// hexToRGB parses a 6-digit RRGGBB hex string into an RGB value.
func hexToRGB(hex string) RGB {
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
		return RGB{}
	}
	return RGB{R: parse(hex[0:2]), G: parse(hex[2:4]), B: parse(hex[4:6])}
}

// Theme is the single shared Loom palette.
var Theme = func() palette {
	// Accent and Cream track Claude's brand palette (Crail / Pampas); CreamAlt and
	// Callout are Pampas-derived tints used only by the PDF renderer. Muted stays a
	// dark warm grey: Claude's "Cloudy" (#B1ADA1) is a muted-UI tint, not a text
	// colour — on the cream background it falls below readable contrast, and Muted
	// drives subtitle/caption/attribution *text* in both the PDF and PPTX.
	p := palette{
		InkHex: "1D1D1B", CreamHex: "F4F3EE", CreamAltHex: "E8E7E0", AccentHex: "C15F3C",
		SageHex: "6F8B6B", GoldHex: "C7A35F", MutedHex: "5F5C54", WhiteHex: "FFFFFF",
		CalloutHex: "F5F0E4",
	}
	p.Ink = hexToRGB(p.InkHex)
	p.Cream = hexToRGB(p.CreamHex)
	p.CreamAlt = hexToRGB(p.CreamAltHex)
	p.Accent = hexToRGB(p.AccentHex)
	p.Sage = hexToRGB(p.SageHex)
	p.Gold = hexToRGB(p.GoldHex)
	p.Muted = hexToRGB(p.MutedHex)
	p.White = hexToRGB(p.WhiteHex)
	p.Callout = hexToRGB(p.CalloutHex)
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

// hexOf renders an RGB value as a 6-digit OOXML hex string.
func hexOf(c RGB) string { return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B) }

// textOnHex returns the legible text color (as OOXML hex) for a hex background.
func textOnHex(bgHex string) string { return hexOf(TextOn(hexToRGB(bgHex))) }
