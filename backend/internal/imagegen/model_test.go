package imagegen

import "testing"

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
	if got.Filename != "a-red-fox-in.png" {
		t.Fatalf("Filename = %q, want a-red-fox-in.png", got.Filename)
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
