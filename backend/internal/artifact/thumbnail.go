package artifact

import (
	"os"
	"strings"

	"github.com/trick77/loom/internal/imagescale"
)

// ThumbnailMaxDimension caps the longest side of a generated artifact thumbnail.
// 144px is 2× the 36px square the artifacts list renders (crisp on HiDPI) and is
// also large enough to reuse in the ~76px composer / sent-message preview boxes.
const ThumbnailMaxDimension = 144

// ThumbnailSuffix is appended to an artifact's path — both its absolute file path
// and its volume-relative path — to form the sidecar thumbnail's path.
const ThumbnailSuffix = ".thumb.jpg"

// rasterImageMIMEs is the set of image MIME types we can decode and shrink into a
// JPEG thumbnail. It mirrors the upload allowlist (PNG/JPEG/WebP/GIF) and the
// formats image generation emits. SVG (image/svg+xml) is deliberately excluded: it
// is vector, already tiny, and needs no raster thumbnail.
var rasterImageMIMEs = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// IsThumbnailableMIME reports whether an artifact's MIME type is a raster image we
// generate a thumbnail for. It ignores any parameters (e.g. a "; charset=" suffix)
// and is case-insensitive.
func IsThumbnailableMIME(mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return rasterImageMIMEs[mime]
}

// WriteThumbnail decodes src, scales it to a JPEG thumbnail, and writes it next to
// the original at origAbsPath+ThumbnailSuffix, returning the thumbnail's
// volume-relative path (origRelPath+ThumbnailSuffix). It overwrites any existing
// sidecar (os.WriteFile truncates) so a partially-written or stale thumbnail from
// an interrupted run or a concurrent request is simply replaced. It returns an
// error when src is not a decodable raster image or the write fails; callers treat
// any error as "no thumbnail" and never fail the parent operation on it.
func WriteThumbnail(src []byte, origAbsPath, origRelPath string) (string, error) {
	data, err := imagescale.Thumbnail(src, ThumbnailMaxDimension)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(origAbsPath+ThumbnailSuffix, data, 0o600); err != nil {
		return "", err
	}
	return origRelPath + ThumbnailSuffix, nil
}
