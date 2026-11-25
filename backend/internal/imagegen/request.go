package imagegen

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DefaultWidth and related constants define the default dimensions and limits for image generation requests.
const (
	DefaultWidth  = 1024
	DefaultHeight = 1024
	// MaxDefaultSide is the longest side an aspect-ratio-derived size may use.
	// Every ratio is fitted to this, so a non-square image is never larger than
	// the square default — the shape changes, the pixel budget does not grow.
	MaxDefaultSide      = 1024
	DefaultOutputFormat = "png"
	MaxPromptRunes      = 4000
	MaxOutputPixels     = 4_000_000
	// MaxInputImages caps how many source images may be forwarded for editing.
	// The FLUX.2 edit endpoints accept up to 4 reference images in image_urls.
	// The dispatcher currently sends one, so this is a guard, not a live limit.
	MaxInputImages = 4
	// MaxInputImageBytes guards each source image against the 20MB per-image
	// input limit. Source images ride along as base64 data URIs, which inflates
	// the request body by roughly a third.
	MaxInputImageBytes = 20 << 20
)

// GenerateRequest is a request to generate an image with specified parameters.
type GenerateRequest struct {
	Prompt   string
	Filename string
	// AspectRatio shapes the output when Width/Height are not given, e.g. "16:9"
	// for a wide scene or "3:4" for a poster. Explicit Width/Height win over it,
	// so a caller asking for exact pixels still gets them. See parseAspect for
	// the form and sizeForAspect for the size a ratio resolves to.
	AspectRatio     string
	Width           int
	Height          int
	Seed            *int64
	OutputFormat    string
	SafetyTolerance *int
	// Model, when non-empty, overrides the provider's configured model for this
	// one request (e.g. routing typography/logo work to FLUX.2 [flex]). Like
	// InputImages it is injected by the dispatcher, never parsed from LLM tool
	// arguments.
	Model string
	// InputImages carries raw source-image bytes to edit/transform directly
	// (e.g. "render a LEGO set from this photo"). When present, the provider
	// sends the pixels to the model alongside the prompt instead of relying on a
	// lossy text re-description. Index 0 is the primary image. Never populated
	// from LLM tool arguments — the dispatcher injects it from the user's upload.
	InputImages [][]byte
}

// GenerateResult is the result of a successful image generation, containing the generated image and metadata.
type GenerateResult struct {
	Filename  string
	Extension string
	MIMEType  string
	Bytes     []byte
	Provider  string
	Model     string
	RequestID string
	Prompt    string
	Seed      *int64
	Width     int
	Height    int
	// CostUSD is what the provider bills for the image; nil when unknown,
	// never a made-up zero.
	CostUSD *float64
}

// Provider is an image generation provider that generates images from requests.
type Provider interface {
	Generate(context.Context, GenerateRequest) (GenerateResult, error)
}

// Normalized returns a validated and normalized version of the request, applying defaults and checking constraints.
func (r GenerateRequest) Normalized() (GenerateRequest, error) {
	out := r
	out.Prompt = strings.TrimSpace(out.Prompt)
	if out.Prompt == "" {
		return GenerateRequest{}, errors.New("prompt is required")
	}
	if len([]rune(out.Prompt)) > MaxPromptRunes {
		return GenerateRequest{}, fmt.Errorf("prompt must be at most %d characters", MaxPromptRunes)
	}
	out.AspectRatio = strings.TrimSpace(out.AspectRatio)
	if out.AspectRatio != "" {
		aspect, ok := parseAspect(out.AspectRatio)
		if !ok {
			return GenerateRequest{}, errors.New("aspect_ratio must be W:H, e.g. 16:9 or 3:1")
		}
		out.Width, out.Height = sizeForAspect(aspect, out.Width, out.Height)
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
		out.SafetyTolerance = new(2)
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

// maxAspect is the most lopsided shape a ratio resolves to. Past it the short
// side falls under the 256 px a provider takes at least, which then raises it
// and changes the shape unseen; a ratio beyond it is drawn at this one.
const maxAspect = 4

// parseAspect reads an aspect ratio as width over height, held within
// maxAspect. The model passes a ratio as the user named it and the image turn
// makes one call, so every common spelling counts: "16:9", "16x9", "16/9",
// "2,35:1", a bare "2.35". Reports false for a value that is no ratio.
func parseAspect(ratio string) (aspect float64, ok bool) {
	ratio = strings.ReplaceAll(strings.TrimSpace(ratio), ",", ".")
	left, right := ratio, "1"
	if i := strings.IndexAny(ratio, ":xX×/"); i >= 0 {
		_, size := utf8.DecodeRuneInString(ratio[i:])
		left, right = ratio[:i], ratio[i+size:]
	}
	a, errA := strconv.ParseFloat(strings.TrimSpace(left), 64)
	b, errB := strconv.ParseFloat(strings.TrimSpace(right), 64)
	// Written as "not above zero" so a NaN term fails too.
	if errA != nil || errB != nil || !(a > 0) || !(b > 0) || math.IsInf(a, 0) || math.IsInf(b, 0) {
		return 0, false
	}
	return min(max(a/b, 1.0/maxAspect), maxAspect), true
}

// sizeForAspect is the pixel size a request of the shape aspect (width over
// height) gets. Without a size: the long side at MaxDefaultSide, the short
// one on the 16 px grid, so a shape never costs more than the square default
// — on a per-megapixel provider a wide or tall one is cheaper than 1:1, not
// dearer. The short side goes to the nearest grid line, not the next: rounded
// up, an edit of an image drew its shape a little squarer each time. A
// partial size ("800 wide, 16:9") completes from the ratio instead of letting
// the missing side fall back to the square default, which would match
// neither what was asked for nor the ratio. A full size is kept.
func sizeForAspect(aspect float64, w, h int) (int, int) {
	switch {
	case w == 0 && h == 0 && aspect >= 1:
		return MaxDefaultSide, round16(MaxDefaultSide / aspect)
	case w == 0 && h == 0:
		return round16(MaxDefaultSide * aspect), MaxDefaultSide
	case h == 0:
		return w, max(1, int(float64(w)/aspect))
	case w == 0:
		return max(1, int(float64(h)*aspect)), h
	}
	return w, h
}

// AspectRatioForSize returns the aspect ratio of (w, h) in lowest terms, used
// to carry a source image's proportions over to an edit so a wide photo does
// not come back square. Zero or negative input falls back to "1:1".
func AspectRatioForSize(w, h int) string {
	if w <= 0 || h <= 0 {
		return "1:1"
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	return strconv.Itoa(w/a) + ":" + strconv.Itoa(h/a)
}

func align16(v int) int {
	if v%16 == 0 {
		return v
	}
	return v + (16 - v%16)
}

// ClampMaxSide scales (w, h) down proportionally so the longest side is at most
// max, preserving aspect ratio. Dimensions already within the bound — including
// the 0×0 "unset" case that Normalized() later defaults to 1024×1024 — are
// returned untouched. Used to fit a request into fal's per-side limit
// (clampToFalBounds).
func ClampMaxSide(w, h, max int) (int, int) {
	if max <= 0 || (w <= max && h <= max) {
		return w, h
	}
	longest := w
	if h > longest {
		longest = h
	}
	scaled := func(v int) int {
		out := v * max / longest
		if out < 1 {
			out = 1
		}
		return out
	}
	return scaled(w), scaled(h)
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

// MIMEType returns the MIME type for the given image format string.
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
