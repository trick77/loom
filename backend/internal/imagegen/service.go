package imagegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/loom/internal/imagescale"
)

// The MCP contract (docs/imagegen.md): server ServerName, tool
// GenerateToolName.
const (
	ServerName       = "imagegen"
	GenerateToolName = "generate"
)

// Arguments the client (loom) fills itself: declared in the schema so any
// server can read them, never shown to the model (loom strips them from the
// tool it offers).
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
	return &Service{provider: provider, defaultModel: normalizeModel(defaultModel), typographyModel: normalizeModel(typographyModel)}
}

// normalizeModel is a model id as the provider reports it back (fal trims
// surrounding slashes), so a locked model compares equal to the configured one.
func normalizeModel(model string) string {
	return strings.Trim(strings.TrimSpace(model), "/")
}

// Description is the tool's description for the model.
func (s *Service) Description() string {
	return toolDescription
}

// Schema is the tool's input schema: what the model fills plus the
// client-filled arguments.
func (s *Service) Schema() map[string]any {
	props := toolProperties()
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
	return map[string]any{"type": "object", "properties": props, "required": []string{"prompt"}}
}

// InputImage is one entry of input_images.
type InputImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

// CallArgs is a call's arguments: the model's and the client's.
type CallArgs struct {
	Prompt          string
	Filename        string
	AspectRatio     string
	Width           int
	Height          int
	Seed            *int64
	OutputFormat    string
	SafetyTolerance *int
	InputImages     []InputImage
	TextHeavy       bool
	Model           string
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

// Call decodes raw arguments and generates. A mistyped optional value (a
// width sent as "1024", a fractional seed) is dropped, not fatal: the image
// is still made, as with any model argument the provider would round.
func (s *Service) Call(ctx context.Context, raw json.RawMessage) (Output, error) {
	var m map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return Output{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	return s.Generate(ctx, ArgsFromMap(m))
}

// ArgsFromMap reads decoded tool arguments. input_images may come as decoded
// JSON ([]any) or as the client built them ([]map[string]any).
func ArgsFromMap(m map[string]any) CallArgs {
	args := CallArgs{
		Prompt:       stringArg(m, "prompt"),
		Filename:     stringArg(m, "filename"),
		AspectRatio:  stringArg(m, "aspect_ratio"),
		OutputFormat: stringArg(m, "output_format"),
		Model:        stringArg(m, ArgModel),
	}
	if w, ok := intArg(m, "width", math.MaxInt32); ok {
		args.Width = int(w)
	}
	if h, ok := intArg(m, "height", math.MaxInt32); ok {
		args.Height = int(h)
	}
	if seed, ok := intArg(m, "seed", math.MaxInt64); ok {
		args.Seed = &seed
	}
	if tol, ok := intArg(m, "safety_tolerance", math.MaxInt32); ok {
		t := int(tol)
		args.SafetyTolerance = &t
	}
	args.TextHeavy, _ = m[ArgTextHeavy].(bool)
	var entries []map[string]any
	switch list := m[ArgInputImages].(type) {
	case []any:
		for _, item := range list {
			entry, _ := item.(map[string]any)
			entries = append(entries, entry)
		}
	case []map[string]any:
		entries = list
	}
	for _, entry := range entries {
		args.InputImages = append(args.InputImages, InputImage{Data: stringArg(entry, "data"), MimeType: stringArg(entry, "mime_type")})
	}
	return args
}

func stringArg(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// intArg reads a whole number of at most limit that a JSON argument carries
// (as float64), or one sent as a plain integer string ("512", not "512px");
// anything else is absent.
func intArg(m map[string]any, key string, limit int64) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		if v == math.Trunc(v) && math.Abs(v) <= float64(limit) {
			return int64(v), true
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n <= limit && n >= -limit {
			return n, true
		}
	}
	return 0, false
}

// Generate renders one image. An edit keeps its source's proportions unless
// the call asked for a shape; a text-heavy first image goes to the typography
// model; a model the conversation already used is kept.
func (s *Service) Generate(ctx context.Context, args CallArgs) (Output, error) {
	if s == nil || s.provider == nil {
		return Output{}, errors.New("image provider is not configured")
	}
	req := GenerateRequest{
		Prompt: args.Prompt, Filename: args.Filename, AspectRatio: args.AspectRatio,
		Width: args.Width, Height: args.Height, Seed: args.Seed,
		OutputFormat: args.OutputFormat, SafetyTolerance: args.SafetyTolerance,
	}
	if len(args.InputImages) > MaxInputImages {
		return Output{}, fmt.Errorf("at most %d input images are supported", MaxInputImages)
	}
	for i, in := range args.InputImages {
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
	result, err := s.provider.Generate(ctx, req)
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
	if m := normalizeModel(args.Model); m != "" {
		return m
	}
	if args.TextHeavy && s.typographyModel != "" {
		return s.typographyModel
	}
	return s.defaultModel
}
