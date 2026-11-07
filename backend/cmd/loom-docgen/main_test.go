package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

func vars(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(vars(map[string]string{"DOCGEN_TOKEN": testToken}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.gotenbergURL != defaultGotenbergURL {
		t.Fatalf("defaults %+v", cfg)
	}
	cfg, err = loadConfig(vars(map[string]string{"DOCGEN_TOKEN": testToken, "DOCGEN_ADDR": ":9000", "DOCGEN_GOTENBERG_URL": " http://localhost:3000 "}))
	if err != nil || cfg.addr != ":9000" || cfg.gotenbergURL != "http://localhost:3000" {
		t.Fatalf("overrides %+v, %v", cfg, err)
	}
	for name, values := range map[string]map[string]string{
		"no token":          {},
		"short token":       {"DOCGEN_TOKEN": "short"},
		"token space":       {"DOCGEN_TOKEN": testToken + " x"},
		"bad gotenberg url": {"DOCGEN_TOKEN": testToken, "DOCGEN_GOTENBERG_URL": "gotenberg:3000"},
	} {
		if _, err := loadConfig(vars(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestServerNeedsTheToken(t *testing.T) {
	h := newServer(config{token: testToken}, generators(nil), "test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
}

// fakeGotenberg answers the health probe and renders every page as a tiny
// PDF, keeping the HTML it got.
type fakeGotenberg struct{ html string }

func (f *fakeGotenberg) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/forms/chromium/convert/html":
			file, _, err := r.FormFile("files")
			if err == nil {
				var buf bytes.Buffer
				_, _ = buf.ReadFrom(file)
				f.html = buf.String()
			}
			_, _ = w.Write([]byte("%PDF-1.7\nfake"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// loomService is loom's own MCP client on the sidecar, its PDFs rendered by
// the fake Gotenberg at gotenbergURL.
func loomService(t *testing.T, gotenbergURL string) *mcp.Service {
	t.Helper()
	gotenberg := docgen.NewGotenbergClient(docgen.GotenbergConfig{BaseURL: gotenbergURL})
	srv := httptest.NewServer(newServer(config{token: testToken}, generators(gotenberg), "test"))
	t.Cleanup(srv.Close)
	svc, err := mcp.NewServiceFromConfigs(context.Background(), mcp.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.DocgenServer: mcp.DocgenServerConfig(srv.URL, testToken),
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// loom, through its own MCP client, gets every tool's file as a document it
// keeps: the contract end to end.
func TestLoomGetsEveryDocument(t *testing.T) {
	gotenberg := &fakeGotenberg{}
	svc := loomService(t, gotenberg.server(t).URL)
	for _, tc := range []struct {
		tool string
		args map[string]any
		name string
		ext  string
	}{
		{"create_text_file", map[string]any{"filename": "notes", "extension": "md", "content": "# Notes"}, "notes.md", "md"},
		{"create_text_file", map[string]any{"filename": "cfg.yaml", "content": "a: 1"}, "cfg.yaml", "yaml"},
		{"create_pdf_file", map[string]any{"filename": "report", "title": "Q3", "content": "# Revenue\n\nUp."}, "report.pdf", "pdf"},
		{"create_xlsx_file", map[string]any{"filename": "table", "rows": []any{[]any{"a", "b"}, []any{"1", "2"}}}, "table.xlsx", "xlsx"},
		{"create_docx_file", map[string]any{"filename": "letter", "content": "# Hi\n\nBody."}, "letter.docx", "docx"},
		{"create_pptx_presentation", map[string]any{"filename": "deck", "slides": []any{map[string]any{"title": "One"}}}, "deck.pptx", "pptx"},
	} {
		got, err := svc.CallToolResult(context.Background(), mcp.ExposedToolName(mcp.DocgenServer, tc.tool), tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if len(got.Documents) != 1 || got.DroppedDocuments != 0 || strings.TrimSpace(got.Text) != "" {
			t.Fatalf("%s: %+v", tc.tool, got)
		}
		if doc := got.Documents[0]; doc.Name != tc.name || doc.Extension != tc.ext || len(doc.Data) == 0 {
			t.Fatalf("%s: document %s .%s (%d bytes)", tc.tool, doc.Name, doc.Extension, len(doc.Data))
		}
	}
	if !strings.Contains(gotenberg.html, "Revenue") {
		t.Fatalf("gotenberg rendered %q", gotenberg.html)
	}
}

// A generator's refusal and a failing Gotenberg are errors the model reads.
func TestErrorsAreToolErrors(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	svc := loomService(t, dead.URL)
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"no content":  {"create_text_file", map[string]any{"filename": "x"}, "content is required"},
		"no renderer": {"create_pdf_file", map[string]any{"filename": "x", "content": "y"}, "gotenberg"},
	} {
		_, err := svc.CallToolResult(context.Background(), mcp.ExposedToolName(mcp.DocgenServer, tc.tool), tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// A whole document fits as the argument: well past the toolkit's 1 MiB
// default, up to the text generator's own cap.
func TestWholeDocumentArgument(t *testing.T) {
	svc := loomService(t, "http://127.0.0.1:0")
	content := strings.Repeat("<p>x</p>\n", docgen.MaxGeneratedInputBytes/9)
	got, err := svc.CallToolResult(context.Background(), mcp.ExposedToolName(mcp.DocgenServer, "create_text_file"),
		map[string]any{"filename": "page", "extension": "html", "content": content})
	if err != nil || len(got.Documents) != 1 || len(got.Documents[0].Data) != len(content) {
		t.Fatalf("documents %d, err %v", len(got.Documents), err)
	}
}

func TestRequireGotenberg(t *testing.T) {
	if err := requireGotenberg(func(context.Context) error { return nil }, "http://g"); err != nil {
		t.Fatal(err)
	}
	err := requireGotenberg(func(context.Context) error { return errors.New("refused") }, "http://g")
	if err == nil || !strings.Contains(err.Error(), "http://g") || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err %v", err)
	}
}

// loom addresses the contract by its own copy of the names, and stores each
// file under its own type for the extension: they must match docgen's.
func TestContractNamesMatchLoom(t *testing.T) {
	if mcp.DocgenServer != docgen.ServerName {
		t.Fatal("loom's docgen server name drifted from docgen's")
	}
	if docgen.MaxFileBytes != artifact.MaxArtifactSizeBytes {
		t.Fatal("docgen's file cap drifted from loom's artifact size")
	}
	for _, ext := range docgen.Extensions() {
		if docgen.MIMEType(ext) != artifact.MIMEType(ext) {
			t.Errorf("%s: docgen sends %q, loom stores %q", ext, docgen.MIMEType(ext), artifact.MIMEType(ext))
		}
	}
}
