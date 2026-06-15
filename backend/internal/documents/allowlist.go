// Package documents turns uploaded files into text for RAG indexing (documents
// via Apache Tika, images via the vision model) and guards which formats may be
// uploaded.
package documents

import (
	"path/filepath"
	"strings"
)

// allowedFormats maps a lower-cased file extension to its canonical MIME type.
// This is the RAG upload allowlist. Document formats are extracted to text by
// Tika (no OCR); image formats bypass Tika and are described by the vision model
// at ingest (see rag.Ingester.extractContent), their MIME strings kept identical
// to httpapi/image_allowlist.go. Keep in sync with the frontend file-chooser `accept`.
var allowedFormats = map[string]string{
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".json": "application/json",
	".html": "text/html; charset=utf-8",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// AllowedFormat reports whether filename's extension is an accepted upload
// format and, if so, returns its canonical MIME type. The check is
// case-insensitive on the extension.
func AllowedFormat(filename string) (mime string, ok bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	mime, ok = allowedFormats[ext]
	return mime, ok
}

// AllowedExtensions returns the accepted extensions (each with leading dot),
// useful for building the frontend file-chooser `accept` attribute.
func AllowedExtensions() []string {
	exts := make([]string, 0, len(allowedFormats))
	for ext := range allowedFormats {
		exts = append(exts, ext)
	}
	return exts
}
