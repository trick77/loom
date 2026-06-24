package imagegen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	DefaultWidth        = 1024
	DefaultHeight       = 1024
	DefaultOutputFormat = "png"
	MaxPromptRunes      = 4000
	MaxOutputPixels     = 4_000_000
	// MaxInputImages caps how many source images may be forwarded for editing.
	// FLUX.2 [klein] (the default model) accepts up to 4 reference images
	// (input_image..input_image_4); the larger [pro]/[max] tiers allow 8. The
	// dispatcher currently sends one, so this is a guard, not a live limit — kept at
	// the klein ceiling to match the configured model.
	MaxInputImages = 4
	// MaxInputImageBytes guards each source image against BFL's 20MB input limit.
	MaxInputImageBytes = 20 << 20
)

type GenerateRequest struct {
	Prompt          string
	Filename        string
	Width           int
	Height          int
	Seed            *int64
	OutputFormat    string
	SafetyTolerance *int
	// InputImages carries raw source-image bytes to edit/transform directly
	// (e.g. "render a LEGO set from this photo"). When present, the provider
	// sends the pixels to the model alongside the prompt instead of relying on a
	// lossy text re-description. Index 0 is the primary image. Never populated
	// from LLM tool arguments — the dispatcher injects it from the user's upload.
	InputImages [][]byte
}

type GenerateResult struct {
	Filename    string
	Extension   string
	MIMEType    string
	Bytes       []byte
	Provider    string
	Model       string
	RequestID   string
	Prompt      string
	Seed        *int64
	Width       int
	Height      int
	CostCredits *float64
}

type Provider interface {
	Generate(context.Context, GenerateRequest) (GenerateResult, error)
}

func (r GenerateRequest) Normalized() (GenerateRequest, error) {
	out := r
	out.Prompt = strings.TrimSpace(out.Prompt)
	if out.Prompt == "" {
		return GenerateRequest{}, errors.New("prompt is required")
	}
	if len([]rune(out.Prompt)) > MaxPromptRunes {
		return GenerateRequest{}, fmt.Errorf("prompt must be at most %d characters", MaxPromptRunes)
	}
	if out.Width == 0 {
		out.Width = DefaultWidth
	}
	if out.Height == 0 {
		out.Height = DefaultHeight
	}
	out.Width = align16(out.Width)
	out.Height = align16(out.Height)
	if out.Width < 64 || out.Height < 64 {
		return GenerateRequest{}, errors.New("width and height must be at least 64 pixels")
	}
	if out.Width*out.Height > MaxOutputPixels {
		return GenerateRequest{}, errors.New("width and height must not exceed 4 megapixels")
	}
	out.OutputFormat = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(out.OutputFormat), "."))
	if out.OutputFormat == "" {
		out.OutputFormat = DefaultOutputFormat
	}
	if out.OutputFormat != "png" && out.OutputFormat != "jpeg" && out.OutputFormat != "jpg" {
		return GenerateRequest{}, errors.New("output_format must be png or jpeg")
	}
	if out.OutputFormat == "jpg" {
		out.OutputFormat = "jpeg"
	}
	if out.SafetyTolerance == nil {
		out.SafetyTolerance = intPtr(2)
	}
	if *out.SafetyTolerance < 0 || *out.SafetyTolerance > 5 {
		return GenerateRequest{}, errors.New("safety_tolerance must be between 0 and 5")
	}
	if len(out.InputImages) > MaxInputImages {
		return GenerateRequest{}, fmt.Errorf("at most %d input images are supported", MaxInputImages)
	}
	for _, img := range out.InputImages {
		if len(img) == 0 {
			return GenerateRequest{}, errors.New("input image is empty")
		}
		if len(img) > MaxInputImageBytes {
			return GenerateRequest{}, errors.New("input image exceeds the 20MB limit")
		}
	}
	out.Filename = normalizeFilename(out.Filename, out.Prompt, out.OutputFormat)
	return out, nil
}

func intPtr(value int) *int {
	return &value
}

func align16(v int) int {
	if v%16 == 0 {
		return v
	}
	return v + (16 - v%16)
}

func normalizeFilename(input, prompt, format string) string {
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	name := strings.TrimSpace(input)
	if name != "" {
		name = filepath.Base(name)
		if current := filepath.Ext(name); current != "" {
			name = strings.TrimSuffix(name, current)
		}
		name = strings.TrimSpace(name)
	}
	if name == "" {
		// No usable filename from the caller: derive a descriptive stem from the
		// prompt so distinct images get distinct names instead of all collapsing
		// to "generated-image" (and piling up as generated-image-2, -3, ...).
		name = slugFromPrompt(prompt)
	}
	if name == "" {
		name = "generated-image"
	}
	return name + "." + ext
}

// slugStopwords are common filler words skipped when building a filename slug so
// the result leans on the descriptive words, e.g. "A red fox in deep snow" -> "red-fox-deep-snow".
var slugStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "in": true, "on": true,
	"at": true, "to": true, "and": true, "or": true, "with": true, "for": true,
	"by": true, "from": true, "as": true, "is": true, "are": true, "be": true,
}

// slugFromPrompt builds a short, filesystem-friendly stem from the first few
// meaningful words of an image prompt, e.g. "A red fox in deep snow" -> "red-fox-deep-snow".
func slugFromPrompt(prompt string) string {
	var all, meaningful []string
	for _, field := range strings.Fields(strings.ToLower(prompt)) {
		var b strings.Builder
		for _, r := range field {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		w := b.String()
		if w == "" {
			continue
		}
		all = append(all, w)
		if !slugStopwords[w] {
			meaningful = append(meaningful, w)
		}
	}
	// Prefer meaningful words; fall back to all words if the prompt is only fillers.
	words := meaningful
	if len(words) == 0 {
		words = all
	}
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, "-")
}

func MIMEType(format string) string {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
