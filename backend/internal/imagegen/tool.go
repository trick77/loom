package imagegen

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Tool struct {
	provider Provider
}

type ToolRequest struct {
	Prompt          string `json:"prompt"`
	Filename        string `json:"filename,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	Seed            *int64 `json:"seed,omitempty"`
	OutputFormat    string `json:"output_format,omitempty"`
	SafetyTolerance *int   `json:"safety_tolerance,omitempty"`
	// Model is injected by the dispatcher (never parsed from LLM tool arguments —
	// note the json:"-") to override the provider's configured model for this
	// request, e.g. routing typography/logo work to FLUX.2 [flex].
	Model string `json:"-"`
	// InputImages is injected by the dispatcher (never parsed from LLM tool
	// arguments — note the json:"-") to forward the user's uploaded photo for
	// direct editing/transformation.
	InputImages [][]byte `json:"-"`
}

type ToolMeta struct {
	DisplayFilename string
	Extension       string
	MIMEType        string
	Provider        string
	Model           string
	RequestID       string
	Width           int
	Height          int
	DurationMs      int64
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func NewTool(provider Provider) Tool {
	return Tool{provider: provider}
}

func (t Tool) ToolName() string {
	return "generate_image"
}

func (t Tool) Schema() ToolSchema {
	return ToolSchema{
		Name:        t.ToolName(),
		Description: "Generate a PNG or JPEG image from a text prompt. Use this when the user asks to create, draw, render, or generate an image. Generate at most one image per turn.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Detailed visual prompt for the generated image.",
				},
				"filename": map[string]any{
					"type":        "string",
					"description": "Optional output filename without path. The extension is added automatically.",
				},
				"width": map[string]any{
					"type":        "integer",
					"description": "Output width in pixels. Defaults to 1024. Rounded up to a multiple of 16.",
				},
				"height": map[string]any{
					"type":        "integer",
					"description": "Output height in pixels. Defaults to 1024. Rounded up to a multiple of 16.",
				},
				"seed": map[string]any{
					"type":        "integer",
					"description": "Optional seed for reproducible generation.",
				},
				"output_format": map[string]any{
					"type":        "string",
					"enum":        []string{"png", "jpeg"},
					"description": "Output image format. Defaults to png.",
				},
				"safety_tolerance": map[string]any{
					"type":        "integer",
					"description": "BFL safety tolerance from 0 to 5. Defaults to 2.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t Tool) Generate(ctx context.Context, req ToolRequest, w io.Writer) (ToolMeta, error) {
	if t.provider == nil {
		return ToolMeta{}, fmt.Errorf("image provider is not configured")
	}
	start := time.Now()
	result, err := t.provider.Generate(ctx, GenerateRequest{
		Prompt:          req.Prompt,
		Filename:        req.Filename,
		Width:           req.Width,
		Height:          req.Height,
		Seed:            req.Seed,
		OutputFormat:    req.OutputFormat,
		SafetyTolerance: req.SafetyTolerance,
		Model:           req.Model,
		InputImages:     req.InputImages,
	})
	if err != nil {
		return ToolMeta{}, err
	}
	durationMs := time.Since(start).Milliseconds()
	if _, err := w.Write(result.Bytes); err != nil {
		return ToolMeta{}, err
	}
	return ToolMeta{
		DisplayFilename: result.Filename,
		Extension:       result.Extension,
		MIMEType:        result.MIMEType,
		Provider:        result.Provider,
		Model:           result.Model,
		RequestID:       result.RequestID,
		Width:           result.Width,
		Height:          result.Height,
		DurationMs:      durationMs,
	}, nil
}
