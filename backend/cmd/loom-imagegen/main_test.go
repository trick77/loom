package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeProvider struct{ got imagegen.GenerateRequest }

func (p *fakeProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	p.got = req
	return imagegen.GenerateResult{Filename: req.Filename, MIMEType: "image/png", Bytes: []byte("\x89PNG\r\n\x1a\nimage"),
		Provider: "fake", Model: req.Model, Width: 1024, Height: 576}, nil
}

func vars(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{"IMAGEGEN_TOKEN": testToken, "IMAGEGEN_API_KEY": "fal-key"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.baseURL != defaultBaseURL || cfg.model != defaultModel || cfg.typographyModel != defaultTypographyModel || cfg.pollTimeout != defaultPollTimeout {
		t.Fatalf("defaults %+v", cfg)
	}
	cfg, err = loadConfig(vars(map[string]string{"IMAGEGEN_TOKEN": testToken, "IMAGEGEN_API_KEY": "k", "IMAGEGEN_TYPOGRAPHY_MODEL": "off", "IMAGEGEN_POLL_TIMEOUT": "2m"}))
	if err != nil || cfg.typographyModel != "" || cfg.pollTimeout != 2*time.Minute {
		t.Fatalf("overrides %+v, %v", cfg, err)
	}
	for name, values := range map[string]map[string]string{
		"short token":   {"IMAGEGEN_TOKEN": "short", "IMAGEGEN_API_KEY": "k"},
		"token space":   {"IMAGEGEN_TOKEN": testToken + " x", "IMAGEGEN_API_KEY": "k"},
		"no api key":    {"IMAGEGEN_TOKEN": testToken},
		"bad base url":  {"IMAGEGEN_TOKEN": testToken, "IMAGEGEN_API_KEY": "k", "IMAGEGEN_BASE_URL": "queue.fal.run"},
		"poll too long": {"IMAGEGEN_TOKEN": testToken, "IMAGEGEN_API_KEY": "k", "IMAGEGEN_POLL_TIMEOUT": "5m"},
	} {
		if _, err := loadConfig(vars(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestServerNeedsTheToken(t *testing.T) {
	h := newServer(config{token: testToken}, imagegen.NewService(&fakeProvider{}, "", ""), "test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
}

// loom, through its own MCP client, gets the sidecar's image with its
// details: the contract end to end.
func TestLoomGetsTheImage(t *testing.T) {
	provider := &fakeProvider{}
	srv := httptest.NewServer(newServer(config{token: testToken}, imagegen.NewService(provider, "pro", "max"), "test"))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.ImagegenServer: {Transport: mcp.TransportStreamableHTTP, URL: srv.URL + "/mcp", Artifacts: true,
			Headers: map[string]string{"Authorization": "Bearer " + testToken}},
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := mcp.ExposedToolName(mcp.ImagegenServer, mcp.ImagegenTool)
	got, err := svc.CallToolResult(context.Background(), name, map[string]any{"prompt": "a logo", "filename": "logo", mcp.ImagegenArgTextHeavy: true})
	if err != nil {
		t.Fatal(err)
	}
	if provider.got.Model != "max" || provider.got.Prompt != "a logo" {
		t.Fatalf("provider got %+v", provider.got)
	}
	if len(got.Images) != 1 {
		t.Fatalf("result %+v", got)
	}
	img := got.Images[0]
	if img.Filename != "logo" || img.MimeType != "image/png" || img.Model != "max" || img.Provider != "fake" || img.Width != 1024 || img.Height != 576 {
		t.Fatalf("image %+v", img)
	}
}

// loom addresses the contract by its own copy of the names: they must match
// the server's.
func TestContractNamesMatchLoom(t *testing.T) {
	if mcp.ImagegenServer != imagegen.ServerName || mcp.ImagegenTool != imagegen.GenerateToolName ||
		mcp.ImagegenArgInputImages != imagegen.ArgInputImages || mcp.ImagegenArgTextHeavy != imagegen.ArgTextHeavy ||
		mcp.ImagegenArgModel != imagegen.ArgModel || mcp.ImagegenMaxInputBytes != imagegen.MaxInputImageBytes ||
		mcp.ImagegenMaxPromptRunes != imagegen.MaxPromptRunes {
		t.Fatal("loom's image contract names drifted from imagegen's")
	}
}
