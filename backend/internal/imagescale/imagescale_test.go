package imagescale

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
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

func TestThumbnail_scalesTheShorterSideToJPEG(t *testing.T) {
	// Given: images far larger than the thumbnail. A square crop fills from the
	// shorter side, so that side lands on 320, whatever the aspect.
	for name, tc := range map[string]struct {
		w, h, wantW, wantH int
	}{
		"4:3":  {1024, 768, 427, 320},
		"16:9": {1920, 1080, 569, 320},
		"9:16": {1080, 1920, 320, 569},
	} {
		in := pngBytes(t, tc.w, tc.h)

		// When
		out, err := Thumbnail(in, 320, 768)

		// Then: a JPEG with the aspect ratio kept.
		if err != nil {
			t.Fatalf("%s: Thumbnail: %v", name, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("%s: decode thumbnail: %v", name, err)
		}
		if format != "jpeg" {
			t.Fatalf("%s: format = %q, want jpeg", name, format)
		}
		if cfg.Width != tc.wantW || cfg.Height != tc.wantH {
			t.Fatalf("%s: dimensions = %dx%d, want %dx%d", name, cfg.Width, cfg.Height, tc.wantW, tc.wantH)
		}
		if len(out) >= len(in) {
			t.Fatalf("%s: thumbnail size = %d, want smaller than original %d", name, len(out), len(in))
		}
	}
}

func TestThumbnail_capsTheLongerSideOfAPanorama(t *testing.T) {
	// 6:1: a 320 shorter side would make it 1920 wide; the cap holds it at 768.
	out, err := Thumbnail(pngBytes(t, 3000, 500), 320, 768)
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if cfg.Width != 768 || cfg.Height != 128 {
		t.Fatalf("dimensions = %dx%d, want 768x128", cfg.Width, cfg.Height)
	}
}

func TestThumbnail_doesNotUpscaleSmallImage(t *testing.T) {
	for name, tc := range map[string]struct{ w, h int }{
		"tiny": {50, 40},
		// Longer than the shorter-side target, but its shorter side is below it.
		"short side small": {600, 300},
	} {
		out, err := Thumbnail(pngBytes(t, tc.w, tc.h), 320, 768)
		if err != nil {
			t.Fatalf("%s: Thumbnail: %v", name, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("%s: decode thumbnail: %v", name, err)
		}
		if format != "jpeg" {
			t.Fatalf("%s: format = %q, want jpeg", name, format)
		}
		if cfg.Width != tc.w || cfg.Height != tc.h {
			t.Fatalf("%s: dimensions = %dx%d, want %dx%d (no upscale)", name, cfg.Width, cfg.Height, tc.w, tc.h)
		}
	}
}

func TestThumbnail_errorsOnUndecodableData(t *testing.T) {
	if _, err := Thumbnail([]byte("<svg/>"), 320, 768); err == nil {
		t.Fatal("Thumbnail of non-raster data = nil error, want error")
	}
}

func TestThumbnail_rejectsDecompressionBomb(t *testing.T) {
	// A tiny PNG header declaring 20000×20000 (400 MP, over the 100 MP cap). Its
	// dimensions are read via DecodeConfig and rejected before the full decode would
	// allocate gigabytes; the caller treats the error as "no thumbnail".
	bomb := pngHeaderWithDimensions(20000, 20000)
	if _, err := Thumbnail(bomb, 320, 768); err == nil {
		t.Fatal("Thumbnail accepted an oversized image; want rejection before decode")
	}
}

func TestDownscaleForModel_skipsDecompressionBomb(t *testing.T) {
	// A real, decodable gray PNG just over the 100 MP cap. All-zero rows deflate
	// to ~100 KB, so it passes any upload size limit, yet a full decode would
	// allocate the whole pixel buffer (gigabytes at e.g. 60000×60000).
	bomb := zeroGrayPNG(t, 10241, 10241)
	out, mime := DownscaleForModel(bomb, "image/png")
	if mime != "image/png" || !bytes.Equal(out, bomb) {
		t.Fatalf("DownscaleForModel decoded an oversized image (mime %q); want it passed through undecoded", mime)
	}
}

// zeroGrayPNG streams an all-black 8-bit grayscale PNG of w×h without building
// the image in memory.
func zeroGrayPNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	row := make([]byte, 1+w) // filter byte + pixels, all zero
	for range h {
		if _, err := zw.Write(row); err != nil {
			t.Fatalf("deflate: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 0 // color type: grayscale
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	for _, c := range []struct {
		name string
		data []byte
	}{{"IHDR", ihdr}, {"IDAT", idat.Bytes()}, {"IEND", nil}} {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(c.data)))
		buf.WriteString(c.name)
		buf.Write(c.data)
		_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(c.name), c.data...)))
	}
	return buf.Bytes()
}

// pngHeaderWithDimensions builds the PNG signature plus a single valid IHDR chunk
// declaring the given dimensions — enough for image.DecodeConfig to report the size
// without any pixel data, so we can exercise the dimension guard cheaply.
func pngHeaderWithDimensions(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	var chunk bytes.Buffer
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(ihdr)))
	chunk.Write(length)
	chunk.WriteString("IHDR")
	chunk.Write(ihdr)
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	chunk.Write(crc)
	buf.Write(chunk.Bytes())
	return buf.Bytes()
}

func TestFitDims(t *testing.T) {
	for _, tc := range []struct{ w, h, max, wantW, wantH int }{
		{100, 50, 200, 100, 50},
		{4000, 2000, 1000, 1000, 500},
		{2000, 4000, 1000, 500, 1000},
		{5000, 1, 1000, 1000, 1},
	} {
		if w, h := fitDims(tc.w, tc.h, tc.max); w != tc.wantW || h != tc.wantH {
			t.Errorf("fitDims(%d, %d, %d) = %d, %d, want %d, %d", tc.w, tc.h, tc.max, w, h, tc.wantW, tc.wantH)
		}
	}
}
