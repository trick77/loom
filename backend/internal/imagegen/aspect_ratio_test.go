package imagegen

import (
	"strings"
	"testing"
)

// Any ratio must resolve to a size the rest of the pipeline accepts unchanged:
// align16 (so Normalized does not silently reshape it), the long side at
// MaxDefaultSide and the short side at the provider's floor or above — picking
// a shape must not quietly cost more on a per-megapixel provider.
func TestAspectSizeIsWellFormed(t *testing.T) {
	for _, ratio := range []string{"1:1", "16:9", "3:1", "1:3", "21:9", "2.35:1", "7:5", "4:1", "10:1", "1:100", "1920:1080", "1e306:1e306", "1e300:1e-300"} {
		aspect, ok := parseAspect(ratio)
		if !ok {
			t.Fatalf("parseAspect(%q) refused", ratio)
		}
		w, h := sizeForAspect(aspect, 0, 0)
		if w%16 != 0 || h%16 != 0 {
			t.Errorf("%s = %dx%d, want both sides a multiple of 16 so align16 leaves them alone", ratio, w, h)
		}
		if max(w, h) != MaxDefaultSide {
			t.Errorf("%s = %dx%d, want the long side at %d", ratio, w, h, MaxDefaultSide)
		}
		if min(w, h) < MaxDefaultSide/maxAspect {
			t.Errorf("%s = %dx%d, want the short side at least %d", ratio, w, h, MaxDefaultSide/maxAspect)
		}
	}
}

func TestParseAspectRefusesWhatIsNoRatio(t *testing.T) {
	for _, ratio := range []string{"", "wide", "16:", ":9", "0:1", "1:0", "0", "-16:9", "16:9:1", "NaN:1", "Inf:1", "1:Inf"} {
		if aspect, ok := parseAspect(ratio); ok {
			t.Errorf("parseAspect(%q) = %v, want it refused", ratio, aspect)
		}
	}
}

// The model passes a ratio as the user named it, and the image turn makes one
// call: a spelling the server cannot read costs the whole image.
func TestParseAspectReadsCommonSpellings(t *testing.T) {
	for _, tc := range []struct {
		ratio string
		want  float64
	}{
		{"16:9", 16.0 / 9}, {"16x9", 16.0 / 9}, {"16 X 9", 16.0 / 9}, {"16×9", 16.0 / 9}, {"16/9", 16.0 / 9},
		{"2,35:1", 2.35}, {"2.35", 2.35}, {"1:3", 1.0 / 3},
		// Two huge equal terms are still a square.
		{"1e306:1e306", 1},
	} {
		if got, ok := parseAspect(tc.ratio); !ok || got != tc.want {
			t.Errorf("parseAspect(%q) = %v, %v; want %v", tc.ratio, got, ok, tc.want)
		}
	}
}

func TestNormalizeResolvesAspectRatio(t *testing.T) {
	for _, tc := range []struct {
		ratio        string
		wantW, wantH int
	}{
		// The seven ratios the tool once listed keep their sizes.
		{"1:1", 1024, 1024},
		{"16:9", 1024, 576},
		{"9:16", 576, 1024},
		{"4:3", 1024, 768},
		{"3:4", 768, 1024},
		{"3:2", 1024, 688},
		{"2:3", 688, 1024},
		// Any other ratio a user names works the same way.
		{"3:1", 1024, 336},
		{"1:3", 336, 1024},
		{"21:9", 1024, 432},
		{"2.35:1", 1024, 432},
		{" 16 : 9 ", 1024, 576},
		{"16x9", 1024, 576},
		// Past 4:1 the short side would fall under the provider's floor, which
		// raises it and changes the shape unseen: the ratio stops at 4:1.
		{"4:1", 1024, 256},
		{"10:1", 1024, 256},
		{"1:10", 256, 1024},
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

// A value that is no ratio is a model mistake, and the error has to show the
// form so the model can correct itself on the retry.
func TestNormalizeRejectsUnreadableAspectRatio(t *testing.T) {
	_, err := GenerateRequest{Prompt: "x", AspectRatio: "wide"}.Normalized()
	if err == nil {
		t.Fatal("Normalized() accepted an aspect ratio that is none")
	}
	for _, want := range []string{"aspect_ratio", "W:H", "16:9"} {
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
		// A shape outside the common ones is kept, not snapped to the nearest:
		// an edited 3:1 banner came back 16:9.
		{"ultrawide keeps its shape", 3440, 1440, "43:18"},
		{"3:1 banner", 1024, 352, "32:11"},
		{"slightly tall keeps its shape", 1000, 1050, "20:21"},
		{"undecodable falls back to square", 0, 0, "1:1"},
		{"negative falls back to square", -4, 3, "1:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AspectRatioForSize(tc.w, tc.h); got != tc.want {
				t.Fatalf("AspectRatioForSize(%d, %d) = %q, want %q", tc.w, tc.h, got, tc.want)
			}
		})
	}
}

// A partial size must complete from the ratio. Letting the missing side fall back
// to the 1024 square default produced a shape matching neither the request nor the
// ratio: "800 wide, 16:9" came out 800x1024, portrait.
func TestNormalizePartialSizeCompletesFromAspectRatio(t *testing.T) {
	for _, tc := range []struct {
		name         string
		w, h         int
		ratio        string
		wantW, wantH int
	}{
		{"width only", 800, 0, "16:9", 800, 464},
		{"height only", 0, 600, "16:9", 1072, 608},
		{"portrait width only", 600, 0, "9:16", 608, 1072},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GenerateRequest{Prompt: "x", Width: tc.w, Height: tc.h, AspectRatio: tc.ratio}.Normalized()
			if err != nil {
				t.Fatalf("Normalized() error = %v", err)
			}
			if got.Width != tc.wantW || got.Height != tc.wantH {
				t.Fatalf("got %dx%d, want %dx%d", got.Width, got.Height, tc.wantW, tc.wantH)
			}
		})
	}
}

// An unusable ratio has to be reported even when a size is also given, so the
// model learns the value was wrong instead of the field being quietly dropped.
func TestNormalizeRejectsUnreadableAspectRatioAlongsideExplicitSize(t *testing.T) {
	if _, err := (GenerateRequest{Prompt: "x", AspectRatio: "0:1", Width: 512, Height: 512}).Normalized(); err == nil {
		t.Fatal("Normalized() silently ignored an aspect ratio that is none")
	}
}

// A partial size next to a ratio outside the common ones completes from the
// ratio itself, not from its size on the 16 px grid at 1024.
func TestNormalizePartialSizeCompletesFromAnyAspectRatio(t *testing.T) {
	got, err := GenerateRequest{Prompt: "x", Width: 1536, AspectRatio: "3:1"}.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Width != 1536 || got.Height != 512 {
		t.Fatalf("got %dx%d, want 1536x512", got.Width, got.Height)
	}
}
