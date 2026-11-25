package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"github.com/trick77/loom/internal/artifact"
)

type recordingProvider struct{ got GenerateRequest }

func (p *recordingProvider) Generate(_ context.Context, req GenerateRequest) (GenerateResult, error) {
	p.got = req
	return GenerateResult{Filename: req.Filename, MIMEType: "image/png", Bytes: []byte("\x89PNG\r\n\x1a\nx"), Provider: "p", Model: req.Model, Width: req.Width, Height: req.Height}, nil
}

func pngOfSize(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// An edit keeps its source's proportions: restyling a 16:9 photo must not
// come back square. An explicit shape still wins.
func TestServiceEditKeepsSourceShape(t *testing.T) {
	p := &recordingProvider{}
	svc := NewService(p, "", "")
	wide := []InputImage{{Data: pngOfSize(t, 1920, 1080), MimeType: "image/png"}}
	if _, err := svc.Generate(context.Background(), CallArgs{Prompt: "watercolor", InputImages: wide}); err != nil {
		t.Fatal(err)
	}
	if p.got.AspectRatio != "16:9" || len(p.got.InputImages) != 1 {
		t.Fatalf("request aspect %q, %d input images; want 16:9 and the source", p.got.AspectRatio, len(p.got.InputImages))
	}
	if _, err := svc.Generate(context.Background(), CallArgs{Prompt: "watercolor", InputImages: wide, AspectRatio: "3:4"}); err != nil {
		t.Fatal(err)
	}
	if p.got.AspectRatio != "3:4" {
		t.Fatalf("explicit aspect lost: %q", p.got.AspectRatio)
	}
	// A shape outside the common ones survives an edit: a generated 3:1
	// banner must not come back 16:9.
	banner := []InputImage{{Data: pngOfSize(t, 1024, 352), MimeType: "image/png"}}
	if _, err := svc.Generate(context.Background(), CallArgs{Prompt: "cyberpunk", InputImages: banner}); err != nil {
		t.Fatal(err)
	}
	got, err := p.got.Normalized()
	if err != nil || got.Width != 1024 || got.Height != 352 {
		t.Fatalf("edit of a 1024x352 banner asks for %dx%d (%v), want its shape kept", got.Width, got.Height, err)
	}
}

func TestServicePicksModel(t *testing.T) {
	cases := []struct {
		name      string
		args      CallArgs
		wantModel string
		clamped   bool
	}{
		{"default", CallArgs{Prompt: "a fox"}, "pro", false},
		{"text heavy", CallArgs{Prompt: "a logo", TextHeavy: true, Width: 2048, Height: 2048}, "max", true},
		// A thread's first picture (a FLUX photo) must not keep a later
		// infographic off the model that renders text: it came back garbled.
		{"text wins over locked", CallArgs{Prompt: "a mind map", TextHeavy: true, Model: "pro"}, "max", true},
		{"locked wins without text", CallArgs{Prompt: "a fox", Model: "legacy"}, "legacy", false},
		// An edit stays with the model that made its source (loom sends it):
		// another model redraws the whole picture in its own look.
		{"edit keeps its model over text", CallArgs{Prompt: "the sign says BAKERY", TextHeavy: true, Model: "pro", InputImages: []InputImage{{Data: "iVBORw0KGgo=", MimeType: "image/png"}}}, "pro", false},
		{"locked typography model is clamped", CallArgs{Prompt: "x", Model: "max", Width: 2048, Height: 1024}, "max", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &recordingProvider{}
			out, err := NewService(p, "pro", "max").Generate(context.Background(), tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if p.got.Model != tc.wantModel || out.Model != tc.wantModel {
				t.Fatalf("model = %q (reported %q), want %q", p.got.Model, out.Model, tc.wantModel)
			}
			if tc.clamped && !nearTypographyArea(p.got.Width, p.got.Height) {
				t.Fatalf("typography size %dx%d, want about %d px", p.got.Width, p.got.Height, typographyPixels)
			}
		})
	}
	// No typography model: text-heavy work stays on the default.
	p := &recordingProvider{}
	if _, err := NewService(p, "pro", "").Generate(context.Background(), CallArgs{Prompt: "a logo", TextHeavy: true}); err != nil || p.got.Model != "pro" {
		t.Fatalf("model = %q, %v", p.got.Model, err)
	}
}

func TestServiceRejectsBadInput(t *testing.T) {
	svc := NewService(&recordingProvider{}, "", "")
	if _, err := svc.Generate(context.Background(), CallArgs{Prompt: "x", InputImages: []InputImage{{Data: "%%%"}}}); err == nil {
		t.Error("non-base64 input image accepted")
	}
	if _, err := svc.Call(context.Background(), json.RawMessage(`{"prompt":`)); err == nil {
		t.Error("broken arguments accepted")
	}
	if _, err := (*Service)(nil).Generate(context.Background(), CallArgs{Prompt: "x"}); err == nil {
		t.Error("nil service generated")
	}
}

func TestServiceCallDecodesArgsAndReportsDetails(t *testing.T) {
	out, err := NewService(&recordingProvider{}, "pro", "").Call(context.Background(), json.RawMessage(`{"prompt":"a fox","filename":"fox","aspect_ratio":"16:9"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.Filename != "fox" || out.MIMEType != "image/png" || out.Model != "pro" || out.Provider != "p" {
		t.Fatalf("output %+v", out)
	}
}

type pricedProvider struct{ cost *float64 }

func (p pricedProvider) Generate(context.Context, GenerateRequest) (GenerateResult, error) {
	return GenerateResult{MIMEType: "image/png", Bytes: []byte("\x89PNG\r\n\x1a\nx"), Model: "m", CostUSD: p.cost}, nil
}

// The provider's price travels on; none stays none, never a zero.
func TestServiceReportsCost(t *testing.T) {
	price := 0.04
	out, err := NewService(pricedProvider{cost: &price}, "", "").Generate(context.Background(), CallArgs{Prompt: "x"})
	if err != nil || out.CostUSD == nil || *out.CostUSD != 0.04 {
		t.Fatalf("CostUSD = %v, %v; want 0.04", out.CostUSD, err)
	}
	out, err = NewService(pricedProvider{}, "", "").Generate(context.Background(), CallArgs{Prompt: "x"})
	if err != nil || out.CostUSD != nil {
		t.Fatalf("CostUSD = %v, %v; want nil", out.CostUSD, err)
	}
}

// The schema declares the loom-filled arguments next to the model's.
func TestServiceSchemaDeclaresLoomArgs(t *testing.T) {
	props := NewService(&recordingProvider{}, "", "").Schema()["properties"].(map[string]any)
	for _, name := range []string{"prompt", ArgInputImages, ArgTextHeavy, ArgModel} {
		if _, ok := props[name]; !ok {
			t.Errorf("schema lacks %s", name)
		}
	}
}

// A mistyped optional value is dropped, the image still made; a numeric
// string counts. An explicit zero safety tolerance is kept.
func TestServiceCallIsLenientWithModelArgs(t *testing.T) {
	p := &recordingProvider{}
	svc := NewService(p, "", "")
	_, err := svc.Call(context.Background(), json.RawMessage(`{"prompt":"x","width":"512","height":"600px","seed":3.5,"safety_tolerance":0,"text_heavy":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.got.Width != 512 || p.got.Height != 0 || p.got.Seed != nil || p.got.SafetyTolerance == nil || *p.got.SafetyTolerance != 0 {
		t.Fatalf("request %+v", p.got)
	}
	// A seed past 32 bits still reproduces.
	if _, err := svc.Call(context.Background(), json.RawMessage(`{"prompt":"x","seed":3000000000}`)); err != nil || p.got.Seed == nil || *p.got.Seed != 3000000000 {
		t.Fatalf("seed %v, %v", p.got.Seed, err)
	}
	// More input images than the provider takes fail, never silently shrink.
	five := make([]InputImage, MaxInputImages+1)
	for i := range five {
		five[i] = InputImage{Data: pngOfSize(t, 4, 4), MimeType: "image/png"}
	}
	if _, err := svc.Generate(context.Background(), CallArgs{Prompt: "x", InputImages: five}); err == nil {
		t.Fatal("too many input images accepted")
	}
	_, err = svc.Call(context.Background(), json.RawMessage(`{"prompt":"x","seed":7,"input_images":[{"data":"`+pngOfSize(t, 8, 8)+`","mime_type":"image/png"}]}`))
	if err != nil || p.got.Seed == nil || *p.got.Seed != 7 || len(p.got.InputImages) != 1 {
		t.Fatalf("request %+v, %v", p.got, err)
	}
}

// nearTypographyArea: within the 16 px grid of the typography target.
func nearTypographyArea(w, h int) bool {
	area := w * h
	return area > typographyPixels*95/100 && area < typographyPixels*105/100
}

// Text needs pixels: the typography model draws every shape at about 2 MP
// (1024×576 leaves a mind map's labels a few pixels per letter). An explicit
// smaller size is kept; a larger one comes down to the target.
func TestServiceTypographySize(t *testing.T) {
	cases := []struct {
		name string
		args CallArgs
		w, h int
	}{
		{"16:9", CallArgs{AspectRatio: "16:9"}, 1920, 1088},
		{"square default", CallArgs{}, 1440, 1440},
		{"4:3", CallArgs{AspectRatio: "4:3"}, 1664, 1248},
		// A ratio outside the common ones gets the text model's pixels too.
		{"3:1", CallArgs{AspectRatio: "3:1"}, 2528, 832},
		{"explicit small kept", CallArgs{Width: 800, Height: 600}, 800, 600},
		{"explicit large scaled down", CallArgs{Width: 2560, Height: 1440}, 1920, 1088},
		{"partial size takes the ratio", CallArgs{Width: 1280, AspectRatio: "16:9"}, 1280, 720},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &recordingProvider{}
			tc.args.Prompt, tc.args.TextHeavy = "a mind map", true
			if _, err := NewService(p, "pro", "max").Generate(context.Background(), tc.args); err != nil {
				t.Fatal(err)
			}
			if p.got.Width != tc.w || p.got.Height != tc.h {
				t.Fatalf("size %dx%d, want %dx%d", p.got.Width, p.got.Height, tc.w, tc.h)
			}
		})
	}
}

// A locked model arrives as the provider reported it (no slashes); it still
// counts as the typography model and is clamped.
func TestServiceNormalizesModelNames(t *testing.T) {
	p := &recordingProvider{}
	if _, err := NewService(p, " /pro/ ", "/max/").Generate(context.Background(), CallArgs{Prompt: "x", Model: "max", Width: 2048, Height: 2048}); err != nil {
		t.Fatal(err)
	}
	if p.got.Model != "max" || !nearTypographyArea(p.got.Width, p.got.Height) {
		t.Fatalf("request %dx%d on %q, want max at about %d px", p.got.Width, p.got.Height, p.got.Model, typographyPixels)
	}
	if _, err := NewService(p, " /pro/ ", "").Generate(context.Background(), CallArgs{Prompt: "x"}); err != nil || p.got.Model != "pro" {
		t.Fatalf("default model %q, %v", p.got.Model, err)
	}
}

// The sidecar sends what loom keeps: its image budget is one artifact.
func TestImageBudgetIsOneArtifact(t *testing.T) {
	if maxImageBytes != artifact.MaxArtifactSizeBytes {
		t.Fatalf("maxImageBytes %d, artifact limit %d", maxImageBytes, artifact.MaxArtifactSizeBytes)
	}
}

func TestServiceDescription(t *testing.T) {
	if NewService(&recordingProvider{}, "", "").Description() == "" {
		t.Fatal("no description")
	}
}
