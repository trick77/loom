package imagescale

import (
	"bytes"
	"encoding/binary"
	"image"
)

// Dimensions reports an image's pixel size as a viewer sees it, swapping width
// and height when an EXIF orientation tag says the stored pixels are rotated a
// quarter turn. It returns 0, 0 for anything it cannot decode; callers treat that
// as "unknown" rather than an error.
//
// The swap matters because image.DecodeConfig reports the *stored* dimensions and
// ignores EXIF entirely. A phone camera writes a portrait photo as landscape
// pixels plus orientation 6, and DownscaleForEditInput passes typical phone photos
// through untouched, tag and all — so without this a portrait photo reads as
// landscape.
func Dimensions(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	if orientationSwapsAxes(exifOrientation(data)) {
		return cfg.Height, cfg.Width
	}
	return cfg.Width, cfg.Height
}

// orientationSwapsAxes reports whether an EXIF orientation value rotates the
// image by a quarter turn, which is what makes the stored width and height read
// the wrong way round. Values 1-4 are upright or mirrored in place.
func orientationSwapsAxes(orientation int) bool {
	return orientation >= 5 && orientation <= 8
}

// exifOrientation extracts the EXIF orientation tag (0x0112) from a JPEG's APP1
// segment, returning 0 when there is no readable tag. It walks only as far as
// IFD0, which is where the tag lives; anything unexpected ends the scan rather
// than erroring, matching this package's best-effort contract.
//
// This is deliberately a small reader rather than a dependency: one tag from one
// well-known offset is all the orientation question needs.
func exifOrientation(data []byte) int {
	app1 := jpegAPP1(data)
	if len(app1) < 14 || !bytes.HasPrefix(app1, []byte("Exif\x00\x00")) {
		return 0
	}
	tiff := app1[6:]
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(tiff, []byte("II\x2a\x00")):
		order = binary.LittleEndian
	case bytes.HasPrefix(tiff, []byte("MM\x00\x2a")):
		order = binary.BigEndian
	default:
		return 0
	}
	ifdOffset := order.Uint32(tiff[4:8])
	// The IFD needs its 2-byte entry count plus at least one 12-byte entry.
	if ifdOffset < 8 || uint64(ifdOffset)+2 > uint64(len(tiff)) {
		return 0
	}
	entries := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	for i := 0; i < entries; i++ {
		start := uint64(ifdOffset) + 2 + uint64(i)*12
		if start+12 > uint64(len(tiff)) {
			return 0
		}
		entry := tiff[start : start+12]
		if order.Uint16(entry[0:2]) != 0x0112 {
			continue
		}
		// Orientation is a SHORT, so the value sits in the first two bytes of the
		// entry's 4-byte value field.
		return int(order.Uint16(entry[8:10]))
	}
	return 0
}

// jpegAPP1 returns the payload of the first APP1 segment of a JPEG, or nil when
// the bytes are not a JPEG or carry no APP1 segment.
func jpegAPP1(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return nil
		}
		marker := data[i+1]
		// Standalone markers carry no length field.
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		// Start of scan: image data follows, so no more metadata segments.
		if marker == 0xDA || marker == 0xD9 {
			return nil
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return nil
		}
		if marker == 0xE1 {
			return data[i+4 : i+2+size]
		}
		i += 2 + size
	}
	return nil
}
