package artifact

import (
	"path/filepath"
	"strings"
)

// allowedImageFormats maps a lower-cased file extension to its canonical MIME
// type for chat image attachments. Scoped to the formats the chat model
// (llm.ModelSummary) accepts AND that we choose to support: PNG, JPG/JPEG,
// WebP, GIF.
// BMP is intentionally excluded. Keep in sync with the image extensions inside
// DOCUMENT_ACCEPT / ATTACHMENT_ACCEPT in ui/src/api.ts.
var allowedImageFormats = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// AllowedImageFormat reports whether filename's extension is an accepted image
// upload format and, if so, returns its canonical MIME type and the bare
// extension (without leading dot). The check is case-insensitive.
func AllowedImageFormat(filename string) (mime string, ext string, ok bool) {
	dotExt := strings.ToLower(filepath.Ext(filename))
	mime, ok = allowedImageFormats[dotExt]
	return mime, strings.TrimPrefix(dotExt, "."), ok
}

// CanonicalImageMIME maps an image type to the accepted canonical type and
// the extension (no dot) it is stored under, forgiving case, parameters and
// the common non-standard "image/jpg". ok is false for any other type.
func CanonicalImageMIME(mimeType string) (mime string, ext string, ok bool) {
	m := bareMIME(mimeType)
	if m == "image/jpg" {
		m = "image/jpeg"
	}
	// The shortest extension of a type wins (.jpg over .jpeg), so the
	// answer does not depend on map order.
	for dotExt, canonical := range allowedImageFormats {
		if canonical == m && (ext == "" || len(dotExt)-1 < len(ext)) {
			ext = strings.TrimPrefix(dotExt, ".")
		}
	}
	if ext == "" {
		return "", "", false
	}
	return m, ext, true
}

// bareMIME lower-cases a Content-Type and drops its parameters.
func bareMIME(mimeType string) string {
	m := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return m
}

// AllowedImageMIME reports whether a client-provided Content-Type names one of
// the accepted image types (ignoring any parameters such as "; charset").
func AllowedImageMIME(mimeType string) bool {
	m := bareMIME(mimeType)
	for _, canonical := range allowedImageFormats {
		if canonical == m {
			return true
		}
	}
	return false
}
