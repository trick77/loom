package artifact

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/trick77/loom/internal/imagescale"
	"github.com/trick77/ship/kit/fsx"
)

// A thumbnail's shorter side is ThumbnailShortSide: the Artifacts library crops
// it into a square tile (object-cover, so the shorter side fills it) of up to
// ~141 CSS px, 282 device px at 2x; a 3x phone's ~115 px tile wants 345. Sized
// by the longer side, a 16:9 image would need a 570 px one for the same. The
// longer side stops at ThumbnailMaxLongSide, so a panorama stays small (and
// softer in a tile). The row icon, composer and sent-message previews reuse it.
const (
	ThumbnailShortSide   = 320
	ThumbnailMaxLongSide = 768
)

// ThumbnailVersion names the thumbnail size. Thumbnails before it were 144 px
// on the longer side, blurry in a tile. It goes into the on-disk path (a stored
// path of another version is regenerated on its next view) and into the URL
// (browsers cache a thumbnail as immutable, so the old URL would keep the old
// bytes for a year).
const ThumbnailVersion = "2"

// thumbnailReservedDir is the per-user reserved subtree that holds sidecar
// thumbnails. It lives under the .loom/ prefix, which user-supplied paths can never
// reach: uploads and generated outputs are filepath.Base'd into server-chosen
// directories, and every path sanitizer rejects a .loom/ prefix. Keeping thumbnails
// here (rather than next to the original as <name>.thumb.jpg) means a thumbnail can
// never collide with — nor, on delete, clobber — a real artifact a user happened to
// name to match the sidecar suffix.
const thumbnailReservedDir = ".loom/thumbnails"

// thumbnailVersionDir holds the current size's thumbnails, inside the reserved
// subtree. Unversioned ones mirror the volume path right under the subtree, and
// a volume path always starts with files/ or projects/, so the two never meet.
const thumbnailVersionDir = thumbnailReservedDir + "/v" + ThumbnailVersion

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
	return thumbnailVersionDir + "/" + filepath.ToSlash(volumeRelPath) + ".jpg"
}

// legacyThumbnailRelPath is where a thumbnail from before ThumbnailVersion sits.
func legacyThumbnailRelPath(volumeRelPath string) string {
	return thumbnailReservedDir + "/" + filepath.ToSlash(volumeRelPath) + ".jpg"
}

// IsCurrentThumbnail reports whether a stored thumbnail path is the current
// size's; any other (an older size, none) wants a fresh one.
func IsCurrentThumbnail(volumeRelPath, thumbnailRelPath string) bool {
	return thumbnailRelPath != "" && thumbnailRelPath == ThumbnailRelPath(volumeRelPath)
}

// ResolveLegacyThumbnail is the absolute path of an artifact's thumbnail from
// before ThumbnailVersion, a fallback while the current size cannot be made.
func ResolveLegacyThumbnail(usersDir, userID, volumeRelPath string) (string, error) {
	return ResolveThumbnailExisting(usersDir, userID, legacyThumbnailRelPath(volumeRelPath))
}

// RemoveLegacyThumbnail best-effort deletes an artifact's thumbnail from before
// ThumbnailVersion, once the current size replaced it.
func RemoveLegacyThumbnail(usersDir, userID, volumeRelPath string) {
	if abs, err := ResolveLegacyThumbnail(usersDir, userID, volumeRelPath); err == nil {
		_ = os.Remove(abs)
	}
}

// ResolveThumbnailExisting maps a stored thumbnail relpath to its absolute path,
// applying the same inside-root guarantee as ResolveExisting. Unlike ResolveExisting
// it operates within the reserved .loom/thumbnails subtree (which ResolveExisting
// forbids for user paths) and requires the relpath to live under that subtree, so a
// stray value can neither escape the user root nor point at a user-visible file.
func ResolveThumbnailExisting(usersDir, userID, thumbnailRelPath string) (string, error) {
	clean, err := cleanVolumePath(thumbnailRelPath)
	if err != nil {
		return "", errors.New("invalid thumbnail path")
	}
	if !strings.HasPrefix(clean, thumbnailReservedDir+"/") {
		return "", errors.New("not a thumbnail path")
	}
	return resolveInside(filepath.Join(usersDir, userID), clean)
}

// WriteThumbnail decodes src, scales it to a JPEG thumbnail, and writes it into the
// reserved thumbnail subtree at ThumbnailRelPath(volumeRelPath), returning that
// volume-relative path. The write is atomic (temp file + rename) so a concurrent
// lazy backfill of the same artifact can never serve a half-written JPEG, and it
// overwrites any existing sidecar. It returns an error when src is not a decodable
// raster image (or is a decompression bomb) or the write fails; callers treat any
// error as "no thumbnail" and never fail the parent operation on it.
func WriteThumbnail(usersDir, userID, volumeRelPath string, src []byte) (string, error) {
	data, err := imagescale.Thumbnail(src, ThumbnailShortSide, ThumbnailMaxLongSide)
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
	if err := fsx.WriteFileAtomic(abs, data, 0o600); err != nil {
		return "", err
	}
	return relPath, nil
}

// GenerateThumbnailBestEffort writes a sidecar thumbnail for a freshly-created
// raster image artifact, returning its volume-relative path (empty for non-raster
// types or on failure). It never propagates an error: a missing thumbnail is
// backfilled lazily by the thumbnail endpoint on first view.
func GenerateThumbnailBestEffort(usersDir, userID, mimeType string, src []byte, volumeRelPath string) string {
	if !IsThumbnailableMIME(mimeType) {
		return ""
	}
	thumbRel, err := WriteThumbnail(usersDir, userID, volumeRelPath, src)
	if err != nil {
		slog.Warn("generate artifact thumbnail failed", "path", volumeRelPath, "err", err)
		return ""
	}
	return thumbRel
}

// RemoveThumbnail best-effort deletes the sidecar thumbnails for an artifact, the
// current size's and an older one never regenerated. It is safe to call for any
// artifact (a non-raster one simply has no sidecar) and never touches anything
// outside the reserved thumbnail subtree, so it cannot remove a real artifact file.
func RemoveThumbnail(usersDir, userID, volumeRelPath string) {
	if abs, err := ResolveThumbnailExisting(usersDir, userID, ThumbnailRelPath(volumeRelPath)); err == nil {
		_ = os.Remove(abs)
	}
	RemoveLegacyThumbnail(usersDir, userID, volumeRelPath)
}
