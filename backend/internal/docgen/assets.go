package docgen

import _ "embed"

// Bundled PDF fonts — the Go typeface (Bigelow & Holmes, BSD; see
// assets/fonts/LICENSE-Go.txt). They are uploaded to Gotenberg as multipart
// assets and referenced by @font-face in the generated HTML (see pdf_html.go), so
// exported PDFs use the brand typeface regardless of the sidecar's system fonts.
// Colour emoji are rendered by Chromium's bundled Noto Color Emoji — not here.

//go:embed assets/fonts/Go-Regular.ttf
var fontGoRegular []byte

//go:embed assets/fonts/Go-Bold.ttf
var fontGoBold []byte

//go:embed assets/fonts/Go-Italic.ttf
var fontGoItalic []byte

//go:embed assets/fonts/Go-Mono.ttf
var fontGoMono []byte

// fontAssets returns the font files to upload alongside index.html, keyed by the
// filenames the CSS @font-face rules reference.
func fontAssets() []gotenbergAsset {
	return []gotenbergAsset{
		{Filename: fontRegularFile, Data: fontGoRegular},
		{Filename: fontBoldFile, Data: fontGoBold},
		{Filename: fontItalicFile, Data: fontGoItalic},
		{Filename: fontMonoFile, Data: fontGoMono},
	}
}
