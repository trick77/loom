package artifact

import "testing"

func TestCanonicalImageMIME(t *testing.T) {
	cases := map[string][2]string{
		"image/png":             {"image/png", "png"},
		"IMAGE/JPEG; charset=x": {"image/jpeg", "jpg"},
		"image/jpg":             {"image/jpeg", "jpg"},
		" image/webp ":          {"image/webp", "webp"},
		"image/gif":             {"image/gif", "gif"},
	}
	for in, want := range cases {
		mime, ext, ok := CanonicalImageMIME(in)
		if !ok || mime != want[0] || ext != want[1] {
			t.Errorf("CanonicalImageMIME(%q) = %q, %q, %v; want %q, %q", in, mime, ext, ok, want[0], want[1])
		}
	}
	for _, in := range []string{"image/svg+xml", "image/bmp", "text/html", ""} {
		if _, _, ok := CanonicalImageMIME(in); ok {
			t.Errorf("CanonicalImageMIME(%q) accepted", in)
		}
	}
}
