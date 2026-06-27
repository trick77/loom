package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/trick77/loom/internal/imagescale"
)

// ThumbnailMaxDimension caps the longest side of a generated artifact thumbnail.
// 144px is 2× the 36px square the artifacts list renders (crisp on HiDPI) and is
// also large enough to reuse in the ~76px composer / sent-message preview boxes.
const ThumbnailMaxDimension = 144

// thumbnailReservedDir is the per-user reserved subtree that holds sidecar
// thumbnails. It lives under the .loom/ prefix, which user-supplied paths can never
// reach: uploads and generated outputs are filepath.Base'd into server-chosen
// directories, and every path sanitizer rejects a .loom/ prefix. Keeping thumbnails
// here (rather than next to the original as <name>.thumb.jpg) means a thumbnail can
// never collide with — nor, on delete, clobber — a real artifact a user happened to
// name to match the sidecar suffix.
const thumbnailReservedDir = ".loom/thumbnails"

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

// ThumbnailRelPath maps an artifact's (collision-free, immutable) volume-relative
// path to its sidecar thumbnail's volume-relative path, mirrored under the reserved
// thumbnail subtree. Because the original relpath is unique, the mirrored path is
// too, so two artifacts can never resolve to the same thumbnail file.
func ThumbnailRelPath(volumeRelPath string) string {
	return thumbnailReservedDir + "/" + filepath.ToSlash(volumeRelPath) + ".jpg"
}

// ResolveThumbnailExisting maps a stored thumbnail relpath to its absolute path,
// applying the same inside-root guarantee as ResolveExisting. Unlike ResolveExisting
// it operates within the reserved .loom/thumbnails subtree (which ResolveExisting
// forbids for user paths) and requires the relpath to live under that subtree, so a
// stray value can neither escape the user root nor point at a user-visible file.
func ResolveThumbnailExisting(usersDir, userID, thumbnailRelPath string) (string, error) {
	slash := filepath.ToSlash(thumbnailRelPath)
	if filepath.IsAbs(thumbnailRelPath) || strings.Contains(slash, "..") {
		return "", errors.New("invalid thumbnail path")
	}
	if !strings.HasPrefix(slash, thumbnailReservedDir+"/") {
		return "", errors.New("not a thumbnail path")
	}
	userRoot := filepath.Join(usersDir, userID)
	abs := filepath.Join(userRoot, filepath.FromSlash(thumbnailRelPath))
	if err := ensureInside(userRoot, abs); err != nil {
		return "", err
	}
	return abs, nil
}

// WriteThumbnail decodes src, scales it to a JPEG thumbnail, and writes it into the
// reserved thumbnail subtree at ThumbnailRelPath(volumeRelPath), returning that
// volume-relative path. The write is atomic (temp file + rename) so a concurrent
// lazy backfill of the same artifact can never serve a half-written JPEG, and it
// overwrites any existing sidecar. It returns an error when src is not a decodable
// raster image (or is a decompression bomb) or the write fails; callers treat any
// error as "no thumbnail" and never fail the parent operation on it.
func WriteThumbnail(usersDir, userID, volumeRelPath string, src []byte) (string, error) {
	data, err := imagescale.Thumbnail(src, ThumbnailMaxDimension)
	if err != nil {
		return "", err
	}
	relPath := ThumbnailRelPath(volumeRelPath)
	abs, err := ResolveThumbnailExisting(usersDir, userID, relPath)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".thumb-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return relPath, nil
}

// RemoveThumbnail best-effort deletes the sidecar thumbnail for an artifact. It is
// safe to call for any artifact (a non-raster one simply has no sidecar) and never
// touches anything outside the reserved thumbnail subtree, so it cannot remove a
// real artifact file.
func RemoveThumbnail(usersDir, userID, volumeRelPath string) {
	if abs, err := ResolveThumbnailExisting(usersDir, userID, ThumbnailRelPath(volumeRelPath)); err == nil {
		_ = os.Remove(abs)
	}
}
