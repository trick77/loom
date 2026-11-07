package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"
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
		{"locked wins over text", CallArgs{Prompt: "a logo", TextHeavy: true, Model: "pro"}, "pro", false},
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
			if tc.clamped && (p.got.Width > maxTypographySide || p.got.Height > maxTypographySide) {
				t.Fatalf("typography size %dx%d not clamped", p.got.Width, p.got.Height)
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
	_, err := svc.Call(context.Background(), json.RawMessage(`{"prompt":"x","width":"512","height":512,"seed":3.5,"safety_tolerance":0,"text_heavy":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.got.Width != 512 || p.got.Height != 512 || p.got.Seed != nil || p.got.SafetyTolerance == nil || *p.got.SafetyTolerance != 0 {
		t.Fatalf("request %+v", p.got)
	}
	_, err = svc.Call(context.Background(), json.RawMessage(`{"prompt":"x","seed":7,"input_images":[{"data":"`+pngOfSize(t, 8, 8)+`","mime_type":"image/png"}]}`))
	if err != nil || p.got.Seed == nil || *p.got.Seed != 7 || len(p.got.InputImages) != 1 {
		t.Fatalf("request %+v, %v", p.got, err)
	}
}

// A locked model arrives as the provider reported it (no slashes); it still
// counts as the typography model and is clamped.
func TestServiceNormalizesModelNames(t *testing.T) {
	p := &recordingProvider{}
	if _, err := NewService(p, " /pro/ ", "/max/").Generate(context.Background(), CallArgs{Prompt: "x", Model: "max", Width: 2048, Height: 2048}); err != nil {
		t.Fatal(err)
	}
	if p.got.Model != "max" || p.got.Width > maxTypographySide {
		t.Fatalf("request %+v, want max clamped", p.got)
	}
	if _, err := NewService(p, " /pro/ ", "").Generate(context.Background(), CallArgs{Prompt: "x"}); err != nil || p.got.Model != "pro" {
		t.Fatalf("default model %q, %v", p.got.Model, err)
	}
}

func TestServiceDescription(t *testing.T) {
	if NewService(&recordingProvider{}, "", "").Description() == "" {
		t.Fatal("no description")
	}
}
