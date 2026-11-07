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

// Call decodes raw arguments and generates. A whole number sent as a string
// ("1024") counts; any other mistyped value ("512px", a fractional seed, a
// width that is an object) is dropped, not fatal: the image is still made.
// Keys match case-insensitively (encoding/json), so a client stripping its
// own arguments must strip every case (loom does).
func (s *Service) Call(ctx context.Context, raw json.RawMessage) (Output, error) {
	var w wireArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &w); err != nil {
			return Output{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	return s.Generate(ctx, w.args())
}

// wireArgs decodes the arguments straight into their fields (an edit source
// is tens of MiB: no map in between). Every field forgives a wrong type: it
// is then absent, never an error.
type wireArgs struct {
	Prompt          looseString     `json:"prompt"`
	Filename        looseString     `json:"filename"`
	AspectRatio     looseString     `json:"aspect_ratio"`
	OutputFormat    looseString     `json:"output_format"`
	Model           looseString     `json:"model"`
	Width           looseInt        `json:"width"`
	Height          looseInt        `json:"height"`
	Seed            looseInt        `json:"seed"`
	SafetyTolerance looseInt        `json:"safety_tolerance"`
	TextHeavy       looseBool       `json:"text_heavy"`
	InputImages     looseInputImage `json:"input_images"`
}

func (w wireArgs) args() CallArgs {
	args := CallArgs{
		Prompt: w.Prompt.v, Filename: w.Filename.v, AspectRatio: w.AspectRatio.v, OutputFormat: w.OutputFormat.v,
		Model: w.Model.v, TextHeavy: w.TextHeavy.v, InputImages: w.InputImages.v,
	}
	if w.Width.ok && w.Width.v <= math.MaxInt32 {
		args.Width = int(w.Width.v)
	}
	if w.Height.ok && w.Height.v <= math.MaxInt32 {
		args.Height = int(w.Height.v)
	}
	if w.Seed.ok {
		seed := w.Seed.v
		args.Seed = &seed
	}
	if w.SafetyTolerance.ok && w.SafetyTolerance.v <= math.MaxInt32 {
		tol := int(w.SafetyTolerance.v)
		args.SafetyTolerance = &tol
	}
	return args
}

type looseString struct{ v string }

func (l *looseString) UnmarshalJSON(b []byte) error {
	_ = json.Unmarshal(b, &l.v)
	return nil
}

type looseBool struct{ v bool }

func (l *looseBool) UnmarshalJSON(b []byte) error {
	_ = json.Unmarshal(b, &l.v)
	return nil
}

// looseInt is a whole number sent as a JSON number or a plain integer
// string ("512", not "512px", not 3.5).
type looseInt struct {
	v  int64
	ok bool
}

func (l *looseInt) UnmarshalJSON(b []byte) error {
	// A wrong type is absent, never an error: the text is read as a number
	// or as a string holding one, and kept only when it is a whole number.
	text := strings.TrimSpace(string(b))
	var s string
	if json.Unmarshal(b, &s) == nil {
		text = strings.TrimSpace(s)
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		l.v, l.ok = v, true
	}
	return nil
}

type looseInputImage struct{ v []InputImage }

func (l *looseInputImage) UnmarshalJSON(b []byte) error {
	_ = json.Unmarshal(b, &l.v)
	return nil
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
