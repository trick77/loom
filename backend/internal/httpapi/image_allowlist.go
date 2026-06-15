package httpapi

import (
	"path/filepath"
	"strings"
)

// allowedImageFormats maps a lower-cased file extension to its canonical MIME
// type for chat image attachments. Scoped to the formats the omnimodal MiMo
// (mimo-v2.5) accepts AND that we choose to support: PNG, JPG/JPEG, WebP, GIF.
// BMP is intentionally excluded. Keep in sync with the image extensions inside
// DOCUMENT_ACCEPT / ATTACHMENT_ACCEPT in ui/src/api.ts.
var allowedImageFormats = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// allowedImageFormat reports whether filename's extension is an accepted image
// upload format and, if so, returns its canonical MIME type and the bare
// extension (without leading dot). The check is case-insensitive.
func allowedImageFormat(filename string) (mime string, ext string, ok bool) {
	dotExt := strings.ToLower(filepath.Ext(filename))
	mime, ok = allowedImageFormats[dotExt]
	return mime, strings.TrimPrefix(dotExt, "."), ok
}

// allowedImageMIME reports whether a client-provided Content-Type names one of
// the accepted image types (ignoring any parameters such as "; charset").
func allowedImageMIME(mimeType string) bool {
	m := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	for _, canonical := range allowedImageFormats {
		if canonical == m {
			return true
		}
	}
	return false
}
