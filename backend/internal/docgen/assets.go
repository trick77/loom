package docgen

import _ "embed"

// Bundled PDF assets. Fonts are "Loom Sans" — the Go typeface with symbol glyphs
// grafted from DejaVu Sans (see assets/fonts/generate.py). Emoji are Twemoji PNGs
// drawn inline as green-check / red-cross status markers. See assets/NOTICE.md.

//go:embed assets/fonts/GoPlus-Regular.ttf
var fontRegular []byte

//go:embed assets/fonts/GoPlus-Bold.ttf
var fontBold []byte

//go:embed assets/fonts/GoPlus-Italic.ttf
var fontItalic []byte

//go:embed assets/fonts/GoPlus-BoldItalic.ttf
var fontBoldItalic []byte

//go:embed assets/emoji/check.png
var emojiCheck []byte

//go:embed assets/emoji/cross.png
var emojiCross []byte
