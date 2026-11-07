package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/trick77/loom/internal/imagescale"
)

// The MCP contract loom addresses (docs/imagegen.md): server ServerName, tool
// GenerateToolName.
const (
	ServerName       = "imagegen"
	GenerateToolName = "generate"
)

// Arguments loom fills itself: declared in the schema so any server can read
// them, never shown to the model (loom strips them from the tool it offers).
const (
	ArgInputImages = "input_images"
	ArgTextHeavy   = "text_heavy"
	ArgModel       = "model"
)

// maxTypographySide caps the typography model's output to the default
// model's ~1024 px ceiling; it would otherwise go up to 4 MP.
const maxTypographySide = 1024

// Service is the image tool behind the MCP contract: the model's arguments
// plus loom's (input images, text-heavy hint, locked model) in, one image and
// its details out. It owns every provider fact: which model renders text
// well, how large that one may draw, what shape an edit keeps.
type Service struct {
	provider        Provider
	defaultModel    string
	typographyModel string
}

// NewService serves provider. defaultModel is the model for an ordinary
// image ("" leaves the provider's own); typographyModel the one for
// text-heavy work ("" keeps every image on the default).
func NewService(provider Provider, defaultModel, typographyModel string) *Service {
	return &Service{provider: provider, defaultModel: strings.TrimSpace(defaultModel), typographyModel: strings.TrimSpace(typographyModel)}
}

// Description is the tool's description for the model.
func (s *Service) Description() string {
	return NewTool(s.provider).Schema().Description
}

// Schema is the tool's input schema: what the model fills plus the
// loom-filled arguments.
func (s *Service) Schema() map[string]any {
	schema := NewTool(s.provider).Schema().Parameters
	props := maps.Clone(schema["properties"].(map[string]any))
	props[ArgInputImages] = map[string]any{
		"type":        "array",
		"description": "Filled by the client: images to edit, the first is the source.",
		"items": map[string]any{"type": "object", "properties": map[string]any{
			"data":      map[string]any{"type": "string", "description": "base64 image bytes"},
			"mime_type": map[string]any{"type": "string"},
		}},
	}
	props[ArgTextHeavy] = map[string]any{"type": "boolean", "description": "Filled by the client: the image must render legible text (a logo, a sign, a poster)."}
	props[ArgModel] = map[string]any{"type": "string", "description": "Filled by the client: the model an earlier image of the conversation used, to keep its look."}
	out := maps.Clone(schema)
	out["properties"] = props
	return out
}

// InputImage is one entry of input_images.
type InputImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

// CallArgs is a call's arguments: the model's and loom's.
type CallArgs struct {
	Prompt          string       `json:"prompt"`
	Filename        string       `json:"filename"`
	AspectRatio     string       `json:"aspect_ratio"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	Seed            *int64       `json:"seed"`
	OutputFormat    string       `json:"output_format"`
	SafetyTolerance *int         `json:"safety_tolerance"`
	InputImages     []InputImage `json:"input_images"`
	TextHeavy       bool         `json:"text_heavy"`
	Model           string       `json:"model"`
}

// Output is one generated image and its details.
type Output struct {
	Filename   string
	MIMEType   string
	Bytes      []byte
	Model      string
	Provider   string
	Width      int
	Height     int
	DurationMs int64
}

// Call decodes raw arguments and generates.
func (s *Service) Call(ctx context.Context, raw json.RawMessage) (Output, error) {
	var args CallArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Output{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	return s.Generate(ctx, args)
}

// Generate renders one image. An edit keeps its source's proportions unless
// the call asked for a shape; a text-heavy first image goes to the typography
// model; a model the conversation already used is kept.
func (s *Service) Generate(ctx context.Context, args CallArgs) (Output, error) {
	if s == nil || s.provider == nil {
		return Output{}, errors.New("image provider is not configured")
	}
	req := ToolRequest{
		Prompt: args.Prompt, Filename: args.Filename, AspectRatio: args.AspectRatio,
		Width: args.Width, Height: args.Height, Seed: args.Seed,
		OutputFormat: args.OutputFormat, SafetyTolerance: args.SafetyTolerance,
	}
	for i, in := range args.InputImages {
		if i == MaxInputImages {
			break
		}
		data, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil || len(data) == 0 {
			return Output{}, fmt.Errorf("input_images[%d] is not base64 image data", i)
		}
		if len(data) > MaxInputImageBytes {
			return Output{}, fmt.Errorf("input_images[%d] is larger than %d MiB", i, MaxInputImageBytes>>20)
		}
		req.InputImages = append(req.InputImages, data)
	}
	if len(req.InputImages) > 0 && req.AspectRatio == "" && req.Width == 0 && req.Height == 0 {
		if w, h := imagescale.Dimensions(req.InputImages[0]); w > 0 && h > 0 {
			req.AspectRatio = AspectRatioForSize(w, h)
		}
	}
	req.Model = s.pickModel(args)
	if s.typographyModel != "" && req.Model == s.typographyModel {
		req.Width, req.Height = ClampMaxSide(req.Width, req.Height, maxTypographySide)
	}
	start := time.Now()
	result, err := s.provider.Generate(ctx, GenerateRequest(req))
	if err != nil {
		return Output{}, err
	}
	return Output{
		Filename: result.Filename, MIMEType: result.MIMEType, Bytes: result.Bytes,
		Model: result.Model, Provider: result.Provider, Width: result.Width, Height: result.Height,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// pickModel keeps the model the conversation already used, else sends
// text-heavy work to the typography model, else the default.
func (s *Service) pickModel(args CallArgs) string {
	if m := strings.TrimSpace(args.Model); m != "" {
		return m
	}
	if args.TextHeavy && s.typographyModel != "" {
		return s.typographyModel
	}
	return s.defaultModel
}
