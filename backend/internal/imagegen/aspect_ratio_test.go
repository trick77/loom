package imagegen

import (
	"strings"
	"testing"
)

// Every advertised ratio must resolve to a size the rest of the pipeline accepts
// unchanged: align16 (so Normalized does not silently reshape it), within the
// per-side envelope, and never larger than the square default — picking a shape
// must not quietly cost more on a per-megapixel provider.
func TestAspectRatioSizesAreWellFormed(t *testing.T) {
	names := AspectRatioNames()
	if len(names) != len(AspectRatioSizes) {
		t.Fatalf("AspectRatioNames has %d entries, AspectRatioSizes has %d — they must list the same ratios",
			len(names), len(AspectRatioSizes))
	}
	square := AspectRatioSizes["1:1"]
	for _, name := range names {
		size, ok := AspectRatioSizes[name]
		if !ok {
			t.Fatalf("AspectRatioNames lists %q but AspectRatioSizes has no entry for it", name)
		}
		w, h := size[0], size[1]
		if w%16 != 0 || h%16 != 0 {
			t.Errorf("%s = %dx%d, want both sides a multiple of 16 so align16 leaves them alone", name, w, h)
		}
		if w > MaxDefaultSide || h > MaxDefaultSide {
			t.Errorf("%s = %dx%d, want both sides at most %d", name, w, h, MaxDefaultSide)
		}
		if w*h > square[0]*square[1] {
			t.Errorf("%s = %dx%d (%d px), want no more pixels than the 1:1 default (%d px)",
				name, w, h, w*h, square[0]*square[1])
		}
	}
}

func TestNormalizeResolvesAspectRatio(t *testing.T) {
	for _, tc := range []struct {
		ratio        string
		wantW, wantH int
	}{
		{"1:1", 1024, 1024},
		{"16:9", 1024, 576},
		{"9:16", 576, 1024},
		{"4:3", 1024, 768},
		{"3:4", 768, 1024},
		{"3:2", 1024, 688},
		{"2:3", 688, 1024},
	} {
		t.Run(tc.ratio, func(t *testing.T) {
			got, err := GenerateRequest{Prompt: "a wide canyon", AspectRatio: tc.ratio}.Normalized()
			if err != nil {
				t.Fatalf("Normalized() error = %v", err)
			}
			if got.Width != tc.wantW || got.Height != tc.wantH {
				t.Fatalf("%s -> %dx%d, want %dx%d", tc.ratio, got.Width, got.Height, tc.wantW, tc.wantH)
			}
		})
	}
}

// Explicit pixels are the escape hatch for "make it exactly 512x512", so they
// must win rather than being overwritten by a ratio the model also guessed at.
func TestNormalizeExplicitSizeBeatsAspectRatio(t *testing.T) {
	got, err := GenerateRequest{Prompt: "x", AspectRatio: "16:9", Width: 512, Height: 512}.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Width != 512 || got.Height != 512 {
		t.Fatalf("got %dx%d, want the explicit 512x512", got.Width, got.Height)
	}
}

func TestNormalizeDefaultsToSquareWithoutAspectRatio(t *testing.T) {
	got, err := GenerateRequest{Prompt: "x"}.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Width != DefaultWidth || got.Height != DefaultHeight {
		t.Fatalf("got %dx%d, want the %dx%d default", got.Width, got.Height, DefaultWidth, DefaultHeight)
	}
}

// An unknown ratio is a model mistake, and the error has to name the accepted
// values so the model can correct itself on the retry.
func TestNormalizeRejectsUnknownAspectRatio(t *testing.T) {
	_, err := GenerateRequest{Prompt: "x", AspectRatio: "21:9"}.Normalized()
	if err == nil {
		t.Fatal("Normalized() accepted an unsupported aspect ratio")
	}
	for _, want := range []string{"aspect_ratio", "16:9", "1:1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestAspectRatioForSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		want string
	}{
		{"exact 16:9 photo", 1920, 1080, "16:9"},
		{"exact portrait", 1080, 1920, "9:16"},
		{"square photo", 800, 800, "1:1"},
		{"4:3 camera frame", 4032, 3024, "4:3"},
		{"3:2 DSLR frame", 6000, 4000, "3:2"},
		{"portrait 2:3", 4000, 6000, "2:3"},
		// Anything more extreme than the widest entry still lands on it rather
		// than falling back to square.
		{"ultrawide clamps to the widest ratio", 3440, 1440, "16:9"},
		// A near-square image must not be dragged to a landscape ratio just
		// because the landscape entries outnumber it on one side.
		{"slightly tall stays close to square", 1000, 1050, "1:1"},
		{"undecodable falls back to square", 0, 0, "1:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AspectRatioForSize(tc.w, tc.h); got != tc.want {
				t.Fatalf("AspectRatioForSize(%d, %d) = %q, want %q", tc.w, tc.h, got, tc.want)
			}
		})
	}
}
