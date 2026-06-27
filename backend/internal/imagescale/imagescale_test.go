package imagescale

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Deterministic LCG noise so the PNG is genuinely incompressible — a regular
	// pattern would compress to a few KB and the "never grow" guard would then
	// (correctly) leave it untouched, which isn't the path we want to exercise.
	seed := uint32(1)
	next := func() uint8 {
		seed = seed*1664525 + 1013904223
		return uint8(seed >> 16)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: next(), G: next(), B: next(), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestDownscaleForModel_resizesAndReencodesLargeImage(t *testing.T) {
	// Given: an image whose longest side far exceeds MaxDimension.
	in := pngBytes(t, 3000, 2000)

	// When
	out, mime := DownscaleForModel(in, "image/png")

	// Then: re-encoded as JPEG, smaller, and capped to MaxDimension on the long side.
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if len(out) >= len(in) {
		t.Fatalf("downscaled size = %d, want smaller than original %d", len(out), len(in))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode downscaled: %v", err)
	}
	longest := cfg.Width
	if cfg.Height > longest {
		longest = cfg.Height
	}
	// Long side scaled to the cap (allow 1px of float rounding), not smaller.
	if longest > MaxDimension || longest < MaxDimension-1 {
		t.Fatalf("longest side = %d, want ~%d", longest, MaxDimension)
	}
	if cfg.Width != longest {
		t.Fatalf("width = %d, want the long side (%d) for a 3:2 image", cfg.Width, longest)
	}
	// Aspect ratio preserved (3:2 → height ~ width*2/3).
	if want := cfg.Width * 2 / 3; cfg.Height < want-2 || cfg.Height > want+2 {
		t.Fatalf("height = %d, want ~%d (3:2 preserved)", cfg.Height, want)
	}
}

func TestDownscaleForModel_leavesSmallImageUntouched(t *testing.T) {
	in := pngBytes(t, 64, 64)
	out, mime := DownscaleForModel(in, "image/png")
	if mime != "image/png" || !bytes.Equal(out, in) {
		t.Fatalf("small image was modified: mime=%q sizeChanged=%v", mime, !bytes.Equal(out, in))
	}
}

func TestDownscaleForModel_returnsInputOnUndecodableData(t *testing.T) {
	in := []byte("not an image")
	out, mime := DownscaleForModel(in, "image/png")
	if mime != "image/png" || !bytes.Equal(out, in) {
		t.Fatalf("undecodable data was modified")
	}
}

func TestThumbnail_scalesLargeImageToJPEGWithinMaxDimension(t *testing.T) {
	// Given: an image far larger than the thumbnail cap.
	in := pngBytes(t, 1024, 768)

	// When
	out, err := Thumbnail(in, 144)

	// Then: a JPEG capped to 144 on the long side, preserving aspect ratio.
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if cfg.Width != 144 {
		t.Fatalf("width = %d, want 144 (long side capped)", cfg.Width)
	}
	if want := 144 * 3 / 4; cfg.Height < want-2 || cfg.Height > want+2 {
		t.Fatalf("height = %d, want ~%d (4:3 preserved)", cfg.Height, want)
	}
	if len(out) >= len(in) {
		t.Fatalf("thumbnail size = %d, want smaller than original %d", len(out), len(in))
	}
}

func TestThumbnail_doesNotUpscaleSmallImage(t *testing.T) {
	in := pngBytes(t, 50, 40)
	out, err := Thumbnail(in, 144)
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if cfg.Width != 50 || cfg.Height != 40 {
		t.Fatalf("dimensions = %dx%d, want 50x40 (no upscale)", cfg.Width, cfg.Height)
	}
}

func TestThumbnail_errorsOnUndecodableData(t *testing.T) {
	if _, err := Thumbnail([]byte("<svg/>"), 144); err == nil {
		t.Fatal("Thumbnail of non-raster data = nil error, want error")
	}
}
