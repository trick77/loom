package imagegen

import "testing"

func TestClampMaxSide(t *testing.T) {
	cases := []struct {
		name      string
		w, h, max int
		wantW     int
		wantH     int
	}{
		{"within bound untouched", 1024, 768, 1024, 1024, 768},
		{"square downscaled", 2048, 2048, 1024, 1024, 1024},
		{"landscape preserves aspect", 2048, 1024, 1024, 1024, 512},
		{"portrait preserves aspect", 1024, 2048, 1024, 512, 1024},
		{"unset zero untouched", 0, 0, 1024, 0, 0},
		{"only one side over", 1536, 256, 1024, 1024, 170},
		{"never collapses below one", 4000, 1, 1024, 1024, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := ClampMaxSide(tc.w, tc.h, tc.max)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("ClampMaxSide(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.w, tc.h, tc.max, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestGenerateRequestNormalize(t *testing.T) {
	req := GenerateRequest{
		Prompt:       "  a clay robot reading a book  ",
		Filename:     "robot",
		Width:        1000,
		Height:       777,
		OutputFormat: "",
	}
	got, err := req.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Prompt != "a clay robot reading a book" {
		t.Fatalf("Prompt = %q", got.Prompt)
	}
	if got.Width != 1008 || got.Height != 784 {
		t.Fatalf("dimensions = %dx%d, want 1008x784", got.Width, got.Height)
	}
	if got.OutputFormat != "png" {
		t.Fatalf("OutputFormat = %q", got.OutputFormat)
	}
	if got.Filename != "robot.png" {
		t.Fatalf("Filename = %q", got.Filename)
	}
	if got.SafetyTolerance == nil || *got.SafetyTolerance != 2 {
		t.Fatalf("SafetyTolerance = %v, want 2", got.SafetyTolerance)
	}
}

func TestGenerateRequestNormalizePreservesStrictSafetyTolerance(t *testing.T) {
	got, err := (GenerateRequest{Prompt: "test", SafetyTolerance: intPtr(0)}).Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.SafetyTolerance == nil || *got.SafetyTolerance != 0 {
		t.Fatalf("SafetyTolerance = %v, want 0", got.SafetyTolerance)
	}
}

func TestGenerateRequestNormalizeRejectsEmptyPrompt(t *testing.T) {
	_, err := (GenerateRequest{Prompt: "   "}).Normalized()
	if err == nil {
		t.Fatal("Normalized() succeeded, want error")
	}
}

func TestGenerateRequestNormalizeRejectsUnsupportedFormat(t *testing.T) {
	_, err := (GenerateRequest{Prompt: "test", OutputFormat: "gif"}).Normalized()
	if err == nil {
		t.Fatal("Normalized() succeeded, want error")
	}
}

func TestGenerateRequestNormalizeDerivesFilenameFromPromptWhenMissing(t *testing.T) {
	got, err := (GenerateRequest{Prompt: "A red fox in deep snow at dawn"}).Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Filename != "red-fox-deep-snow.png" {
		t.Fatalf("Filename = %q, want red-fox-deep-snow.png", got.Filename)
	}
}

func TestGenerateRequestNormalizeFallsBackToFillerWordsWhenNoMeaningfulWords(t *testing.T) {
	got, err := (GenerateRequest{Prompt: "the and of in"}).Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Filename != "the-and-of-in.png" {
		t.Fatalf("Filename = %q, want the-and-of-in.png", got.Filename)
	}
}

func TestGenerateRequestNormalizeFallsBackToGeneratedImageWhenPromptHasNoWordChars(t *testing.T) {
	got, err := (GenerateRequest{Prompt: "!!! ??? ..."}).Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if got.Filename != "generated-image.png" {
		t.Fatalf("Filename = %q, want generated-image.png", got.Filename)
	}
}
