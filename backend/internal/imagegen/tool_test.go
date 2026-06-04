package imagegen

import (
	"bytes"
	"context"
	"testing"
)

type fakeProvider struct {
	req GenerateRequest
}

func (f *fakeProvider) Generate(_ context.Context, req GenerateRequest) (GenerateResult, error) {
	normalized, err := req.Normalized()
	if err != nil {
		return GenerateResult{}, err
	}
	f.req = normalized
	return GenerateResult{
		Filename:  normalized.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("png-bytes"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    normalized.Prompt,
		Width:     normalized.Width,
		Height:    normalized.Height,
	}, nil
}

func TestToolSchema(t *testing.T) {
	tool := NewTool(&fakeProvider{})
	schema := tool.Schema()
	if schema.Name != "generate_image" {
		t.Fatalf("Name = %q", schema.Name)
	}
	if schema.Parameters["type"] != "object" {
		t.Fatalf("Parameters = %#v", schema.Parameters)
	}
}

func TestToolGenerateWritesImage(t *testing.T) {
	provider := &fakeProvider{}
	tool := NewTool(provider)
	var out bytes.Buffer
	meta, err := tool.Generate(context.Background(), ToolRequest{
		Prompt:   "a robot",
		Filename: "robot",
		Width:    512,
		Height:   512,
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if out.String() != "png-bytes" {
		t.Fatalf("written bytes = %q", out.String())
	}
	if meta.DisplayFilename != "robot.png" || meta.Extension != "png" || meta.MIMEType != "image/png" {
		t.Fatalf("meta = %#v", meta)
	}
	if provider.req.Prompt != "a robot" || provider.req.Width != 512 {
		t.Fatalf("provider request = %#v", provider.req)
	}
}

func TestToolGeneratePassesExplicitSafetyToleranceZero(t *testing.T) {
	provider := &fakeProvider{}
	tool := NewTool(provider)
	var out bytes.Buffer
	zero := 0
	_, err := tool.Generate(context.Background(), ToolRequest{
		Prompt:          "a robot",
		SafetyTolerance: &zero,
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if provider.req.SafetyTolerance == nil || *provider.req.SafetyTolerance != 0 {
		t.Fatalf("SafetyTolerance = %v, want 0", provider.req.SafetyTolerance)
	}
}
