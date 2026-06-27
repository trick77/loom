// Package imagescale shrinks user-uploaded images before they are inlined as
// base64 data URLs into a chat request. Vision models tile an image to a fixed
// token budget, so a multi-megabyte original carries no signal a ~1-2 MP JPEG
// lacks — but the raw bytes would otherwise ride (re-encoded ~1.33x as base64) on
// every tool round of a vision turn. Downscaling here bounds that payload.
package imagescale

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register GIF decoder (first frame is used)
	"image/jpeg"
	_ "image/png" // register PNG decoder

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WebP decoder (decode-only)
)

const (
	// MaxDimension caps the longest side of the image sent to the model. Vision
	// models cap effective resolution by tiling, so larger inputs are wasted bytes.
	MaxDimension = 1568
	// softByteCap recompresses images that are already small in dimensions but
	// still heavy on disk (e.g. a low-resolution but lossless PNG photo).
	softByteCap = 1 << 20 // 1 MiB
	jpegQuality = 85
	// editMaxDimension bounds source images forwarded to the image model for
	// direct editing/transformation. Unlike the vision-input path, detail is the
	// whole point here, so the cap is generous — it only trims images that would
	// breach BFL's ~20MP input envelope (a 4096-long side stays well under 20MP at
	// common aspect ratios). Typical phone photos pass through untouched.
	editMaxDimension = 4096
	// editByteCap matches BFL's 20MB per-image input limit.
	editByteCap = 20 << 20
)

// DownscaleForModel decodes an image and, when it exceeds the model-input budget,
// resizes it (longest side <= MaxDimension) and/or re-encodes it as JPEG to shrink
// the inline payload. It is best-effort: on any decode/encode failure — or when
// recompression would not actually be smaller — it returns the input bytes and
// MIME unchanged, so a chat turn never fails because of resizing. The returned
// MIME is what the data URL should advertise.
func DownscaleForModel(data []byte, mimeType string) ([]byte, string) {
	return fitWithin(data, mimeType, MaxDimension, softByteCap)
}

// DownscaleForEditInput bounds a source image forwarded to the image model for
// direct editing to BFL's input envelope, preserving as much detail as possible.
// Like DownscaleForModel it is best-effort and returns the input unchanged when it
// already fits or when recompression cannot decode/shrink it.
func DownscaleForEditInput(data []byte, mimeType string) ([]byte, string) {
	return fitWithin(data, mimeType, editMaxDimension, editByteCap)
}

// fitWithin resizes (longest side <= maxDimension) and/or re-encodes as JPEG only
// when the input exceeds maxDimension or byteCap; otherwise it returns the bytes
// and MIME unchanged.
func fitWithin(data []byte, mimeType string, maxDimension, byteCap int) ([]byte, string) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mimeType
	}
	src := img.Bounds()
	w, h := src.Dx(), src.Dy()
	if w == 0 || h == 0 {
		return data, mimeType
	}
	longest := w
	if h > longest {
		longest = h
	}
	if longest <= maxDimension && len(data) <= byteCap {
		return data, mimeType
	}

	nw, nh := w, h
	if longest > maxDimension {
		scale := float64(maxDimension) / float64(longest)
		nw = max(1, int(float64(w)*scale))
		nh = max(1, int(float64(h)*scale))
	}

	out, err := scaleToJPEG(img, src, nw, nh)
	if err != nil {
		return data, mimeType
	}
	if len(out) >= len(data) {
		return data, mimeType
	}
	return out, "image/jpeg"
}

// maxThumbnailSourcePixels caps the pixel area of an image we will fully decode to
// build a thumbnail. It is a decompression-bomb guard, not a typical-photo limit:
// at 100 MP it sits far above a 4096² (~16 MP) edit input or any real camera, so it
// rejects only crafted inputs (a tiny file declaring e.g. 30000×30000) that would
// otherwise allocate gigabytes on decode. A rejected image simply gets no thumbnail.
const maxThumbnailSourcePixels = 100 << 20

// Thumbnail decodes data and produces a small JPEG whose longest side is at most
// maxDimension, flattening any transparency onto white. Unlike DownscaleForModel
// it ALWAYS re-encodes as JPEG (callers serve the result with a fixed image/jpeg
// content type) and returns an error for input it cannot decode as a raster image
// (SVG, corrupt bytes); callers treat that as "no thumbnail" and fall back to the
// original. An image already within maxDimension is re-encoded at its current size
// rather than upscaled.
func Thumbnail(data []byte, maxDimension int) ([]byte, error) {
	// Read the declared dimensions cheaply (no pixel allocation) and reject a
	// decompression bomb before the full decode below would blow up memory.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxThumbnailSourcePixels {
		return nil, errors.New("imagescale: source image too large to thumbnail")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	src := img.Bounds()
	w, h := src.Dx(), src.Dy()
	if w == 0 || h == 0 {
		return nil, errors.New("imagescale: image has zero dimension")
	}
	nw, nh := w, h
	longest := w
	if h > longest {
		longest = h
	}
	if longest > maxDimension {
		scale := float64(maxDimension) / float64(longest)
		nw = max(1, int(float64(w)*scale))
		nh = max(1, int(float64(h)*scale))
	}
	return scaleToJPEG(img, src, nw, nh)
}

// scaleToJPEG resizes the src region of img into an nw×nh canvas and encodes it as
// JPEG. Any transparency is flattened onto white first, since JPEG has no alpha and
// would otherwise render transparent regions as black.
func scaleToJPEG(img image.Image, src image.Rectangle, nw, nh int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
