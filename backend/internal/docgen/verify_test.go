package docgen

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Gated end-to-end render against a real Gotenberg sidecar, for manual visual
// inspection. Skipped unless BACKEND_GOTENBERG_URL and OUT_DIR are set, e.g.:
//
//	docker run --rm -p 3000:3000 gotenberg/gotenberg:8
//	BACKEND_GOTENBERG_URL=http://localhost:3000 OUT_DIR=/tmp \
//	  go test ./internal/docgen -run TestGenerateVerifyPDFs -v
func TestGenerateVerifyPDFs(t *testing.T) {
	base := os.Getenv("BACKEND_GOTENBERG_URL")
	outDir := os.Getenv("OUT_DIR")
	if base == "" || outDir == "" {
		t.Skip("set BACKEND_GOTENBERG_URL and OUT_DIR for the e2e render")
	}
	gen := NewPDFGenerator(NewGotenbergClient(GotenbergConfig{BaseURL: base}))

	cases := map[string]map[string]any{
		"verify-markers.pdf": {
			"title":    "Bifrost vs LiteLLM: Gated Features Comparison",
			"subtitle": "Enterprise-only features for AI gateway procurement evaluation",
			"blocks": []any{
				map[string]any{"type": "paragraph", "text": "The ✓ marks below show where each product delivers natively; ✗ marks a gap."},
				map[string]any{"type": "table", "rows": []any{
					[]any{"Feature", "Bifrost Enterprise", "LiteLLM Enterprise"},
					[]any{"SSO / SAML / OIDC", "✅️ SAML SSO, RBAC, and policy enforcement across every team and workspace in the org", "✓ SSO, SCIM"},
					[]any{"MCP gateway", "✔ Supported", "❌ Not available"},
					[]any{"Adaptive load balancing", "✓", "✗"},
				}},
				map[string]any{"type": "bullets", "items": []any{
					"✓ Included in every tier at no additional cost",
					"✗ Not available without an Enterprise contract",
					"Plain bullet with no marker 🚀",
				}},
			},
		},
		"verify-code.pdf": {
			"title": "Code block rendering",
			"blocks": []any{
				map[string]any{"type": "heading", "level": float64(2), "text": "High-Level Architecture"},
				map[string]any{"type": "code", "text": "┌─────────────────────────────────────────────┐\n│ Web UI (Loom-like)                          │\n│   Sidebar: Epics → Features → Specs         │\n│   Chat panel (RAG-grounded Q&A)             │\n└─────────────────────────────────────────────┘"},
				map[string]any{"type": "code", "language": "go", "text": "func main() {\n\tfmt.Println(\"hello, world\")\n}"},
			},
		},
	}
	for name, payload := range cases {
		var buf bytes.Buffer
		if _, err := gen.Generate(GenerateRequest{Filename: name, Payload: payload, Context: context.Background()}, &buf); err != nil {
			t.Fatalf("Generate(%s): %v", name, err)
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
			t.Fatalf("%s: not a PDF", name)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		t.Logf("wrote %s (%d bytes)", name, buf.Len())
	}
}
