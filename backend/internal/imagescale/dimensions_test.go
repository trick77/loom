package imagescale

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// jpegWithOrientation encodes a w×h JPEG and splices an EXIF APP1 segment
// carrying the given orientation tag directly after SOI, which is where a camera
// writes it.
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	// TIFF header (little-endian) + one IFD entry: tag 0x0112, type SHORT, count 1.
	var tiff bytes.Buffer
	tiff.WriteString("II\x2a\x00")
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8)) // IFD0 offset
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1)) // entry count
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(orientation))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0)) // pad the value field
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0)) // next IFD: none

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write(raw.Bytes()[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(raw.Bytes()[2:])
	return out.Bytes()
}

// A phone writes a portrait photo as landscape pixels plus an orientation tag,
// and DownscaleForEditInput forwards typical phone photos untouched — so reading
// the stored dimensions alone reports the wrong shape for the single most common
// upload there is.
func TestDimensionsHonoursEXIFOrientation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		orientation  int
		wantW, wantH int
	}{
		{"no rotation", 1, 400, 300},
		{"mirrored horizontally, still landscape", 2, 400, 300},
		{"rotated 180, still landscape", 3, 400, 300},
		{"rotated 90 CW reads as portrait", 6, 300, 400},
		{"rotated 90 CCW reads as portrait", 8, 300, 400},
		{"transposed reads as portrait", 5, 300, 400},
		{"transversed reads as portrait", 7, 300, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h := Dimensions(jpegWithOrientation(t, 400, 300, tc.orientation))
			if w != tc.wantW || h != tc.wantH {
				t.Fatalf("Dimensions() = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

// A format with no EXIF at all must report its stored size unchanged rather than
// being tripped up by the APP1 scan.
func TestDimensionsReadsPNGWithoutEXIF(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if w, h := Dimensions(buf.Bytes()); w != 640 || h != 480 {
		t.Fatalf("Dimensions() = %dx%d, want 640x480", w, h)
	}
}

func TestDimensionsReturnsZeroForUndecodableBytes(t *testing.T) {
	if w, h := Dimensions([]byte("not an image")); w != 0 || h != 0 {
		t.Fatalf("Dimensions() = %dx%d, want 0x0", w, h)
	}
}

// The APP1 walk runs over attacker-supplied bytes, so a truncated or lying
// segment must end the scan rather than panic on a slice bound.
func TestExifOrientationSurvivesMalformedSegments(t *testing.T) {
	valid := jpegWithOrientation(t, 400, 300, 6)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"soi only", []byte{0xFF, 0xD8}},
		{"truncated mid-APP1", valid[:12]},
		{"length field longer than the buffer", append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}, valid[6:20]...)},
		{"zero length field", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x00, 0x00}},
		{"not a jpeg", []byte("PK\x03\x04 zip, actually")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Must return, not panic; the value itself is don't-care.
			_ = exifOrientation(tc.data)
		})
	}
}
