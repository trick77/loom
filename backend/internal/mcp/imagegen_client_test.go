package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/trick77/loom/internal/imagegen"
)

type stubImageProvider struct {
	result imagegen.GenerateResult
	err    error
	got    imagegen.GenerateRequest
}

func (p *stubImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	p.got = req
	return p.result, p.err
}

func imagegenService(t *testing.T, p imagegen.Provider) *Service {
	t.Helper()
	svc, err := NewServiceFromConfigs(context.Background(), Config{Servers: map[string]ServerConfig{
		imagegen.ServerName: ImagegenServerConfig(imagegen.NewService(p, "pro", "")),
	}}, Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// The built-in image server answers the contract: imagegen__generate, an
// artifact server, its image back as an Image.
func TestImagegenClientServesTheContract(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage")
	p := &stubImageProvider{result: imagegen.GenerateResult{Filename: "out/fox", MIMEType: "image/png", Bytes: png, Model: "pro", Provider: "fal", Width: 64, Height: 32}}
	svc := imagegenService(t, p)
	if !svc.HasTool("imagegen__generate") || !svc.MakesArtifacts("imagegen__generate") {
		t.Fatal("imagegen__generate missing or not an artifact tool")
	}
	got, err := svc.CallToolResult(context.Background(), "imagegen__generate", map[string]any{"prompt": "a fox", "model": "pro"})
	if err != nil {
		t.Fatal(err)
	}
	if p.got.Prompt != "a fox" || p.got.Model != "pro" {
		t.Fatalf("provider got %+v", p.got)
	}
	if len(got.Images) != 1 || got.Text != "" {
		t.Fatalf("result %+v, want one image and no text", got)
	}
	img := got.Images[0]
	if img.Filename != "fox" || img.MimeType != "image/png" || img.Extension != "png" || img.Model != "pro" || img.Provider != "fal" || img.Width != 64 {
		t.Fatalf("image %+v", img)
	}
}

func TestImagegenClientRefusesUnusableOutput(t *testing.T) {
	svc := imagegenService(t, &stubImageProvider{result: imagegen.GenerateResult{MIMEType: "image/png", Bytes: []byte("<html>")}})
	got, err := svc.CallToolResult(context.Background(), "imagegen__generate", map[string]any{"prompt": "x"})
	if err != nil || len(got.Images) != 0 || got.DroppedImages != 1 {
		t.Fatalf("got %+v, %v; want the image refused and counted", got, err)
	}
	failing := imagegenService(t, &stubImageProvider{err: errors.New("provider down")})
	if _, err := failing.CallToolResult(context.Background(), "imagegen__generate", map[string]any{"prompt": "x"}); err == nil {
		t.Fatal("provider error swallowed")
	}
	client := &imagegenClient{serverName: "imagegen", svc: imagegen.NewService(&stubImageProvider{}, "", "")}
	if _, err := client.CallTool(context.Background(), "other", nil); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}
