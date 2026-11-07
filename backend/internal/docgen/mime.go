package docgen

// mimeTypes are the types of the files the generators make, by extension, as
// loom stores them (its artifact.MIMEType; loom-docgen's tests hold the two
// together). loom keeps a file only when it comes with its extension's type.
var mimeTypes = map[string]string{
	"pdf":  "application/pdf",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"csv":  "text/csv; charset=utf-8",
	"html": "text/html; charset=utf-8",
	"json": "application/json; charset=utf-8",
	"xml":  "application/xml; charset=utf-8",
	"md":   "text/markdown; charset=utf-8",
}

// MIMEType is the type of a generated file with extension ext; any text
// extension without its own type is plain text.
func MIMEType(ext string) string {
	if t, ok := mimeTypes[ext]; ok {
		return t
	}
	return "text/plain; charset=utf-8"
}

// Extensions are the extensions the generators may produce: the binary
// formats and every text extension create_text_file allows.
func Extensions() []string {
	out := []string{"pdf", "docx", "xlsx", "pptx"}
	for ext := range allowedTextExtensions {
		out = append(out, ext)
	}
	return out
}
