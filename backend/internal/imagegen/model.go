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
)

type GenerateRequest struct {
	Prompt          string
	Filename        string
	Width           int
	Height          int
	Seed            *int64
	OutputFormat    string
	SafetyTolerance *int
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
	out.Filename = normalizeFilename(out.Filename, out.OutputFormat)
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

func normalizeFilename(input, format string) string {
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	name := strings.TrimSpace(input)
	if name == "" {
		name = "generated-image"
	}
	name = filepath.Base(name)
	if current := filepath.Ext(name); current != "" {
		name = strings.TrimSuffix(name, current)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "generated-image"
	}
	return name + "." + ext
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
