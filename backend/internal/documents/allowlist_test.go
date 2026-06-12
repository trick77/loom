package documents

import "testing"

func TestAllowedFormat_acceptsCoreFormats(t *testing.T) {
	cases := map[string]string{
		"report.pdf":      "application/pdf",
		"notes.MD":        "text/markdown; charset=utf-8",
		"data.csv":        "text/csv; charset=utf-8",
		"sheet.xlsx":      "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"deck.pptx":       "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"letter.docx":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"page.html":       "text/html; charset=utf-8",
		"config.json":     "application/json",
		"readme.txt":      "text/plain; charset=utf-8",
	}
	for name, wantMIME := range cases {
		mime, ok := AllowedFormat(name)
		if !ok {
			t.Errorf("AllowedFormat(%q) ok = false, want true", name)
			continue
		}
		if mime != wantMIME {
			t.Errorf("AllowedFormat(%q) mime = %q, want %q", name, mime, wantMIME)
		}
	}
}

func TestAllowedFormat_rejectsDisallowed(t *testing.T) {
	for _, name := range []string{"malware.exe", "archive.zip", "photo.png", "movie.mp4", "noext", "script.sh"} {
		if _, ok := AllowedFormat(name); ok {
			t.Errorf("AllowedFormat(%q) ok = true, want false", name)
		}
	}
}
