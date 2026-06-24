// Package imagescale shrinks user-uploaded images before they are inlined as
// base64 data URLs into a chat request. Vision models tile an image to a fixed
// token budget, so a multi-megabyte original carries no signal a ~1-2 MP JPEG
// lacks — but the raw bytes would otherwise ride (re-encoded ~1.33x as base64) on
// every tool round of a vision turn. Downscaling here bounds that payload.
package imagescale

import (
	"bytes"
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

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// Flatten any transparency onto white so JPEG (which has no alpha) does not
	// render transparent regions as black.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return data, mimeType
	}
	if out.Len() >= len(data) {
		return data, mimeType
	}
	return out.Bytes(), "image/jpeg"
}
