package docgen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeForPDFNormalizesMarkers(t *testing.T) {
	cases := map[string]string{
		"✅️ done":             "✓ done",  // emoji check + variation selector
		"❌ nope":              "✗ nope",  // cross mark emoji
		"✔ a ✘ b":             "✓ a ✗ b", // heavy check / heavy cross
		"☑ x ☒ y":             "✓ x ✗ y", // ballot boxes
		"✕ ✖":                 "✗ ✗",     // multiplication crosses
		"plain text":          "plain text",
		"arrow → dash — kept": "arrow → dash — kept", // covered symbols pass through
	}
	for in, want := range cases {
		if got := sanitizeForPDF(in); got != want {
			t.Errorf("sanitizeForPDF(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeForPDFStripsNonBMP(t *testing.T) {
	// A non-BMP emoji with no marker mapping becomes the replacement char.
	if got := sanitizeForPDF("a\U0001F600b"); got != "a�b" {
		t.Errorf("got %q", got)
	}
	// A non-BMP checkmark variant is normalized, not replaced.
	if got := sanitizeForPDF("\U0001F5F8 ok"); got != "✓ ok" {
		t.Errorf("got %q", got)
	}
}

func TestSplitMarker(t *testing.T) {
	tests := []struct {
		in           string
		found, check bool
		rest         string
	}{
		{"✓ yes", true, true, "yes"},
		{"✗ no", true, false, "no"},
		{"✓", true, true, ""},
		{"  ✓  spaced", true, true, "spaced"},
		{"no marker here", false, false, "no marker here"},
		{"", false, false, ""},
	}
	for _, tc := range tests {
		found, check, rest := splitMarker(tc.in)
		if found != tc.found || check != tc.check || rest != tc.rest {
			t.Errorf("splitMarker(%q) = (%v,%v,%q), want (%v,%v,%q)", tc.in, found, check, rest, tc.found, tc.check, tc.rest)
		}
	}
}

func markerPayload(withMarkers bool) map[string]any {
	cell := "Supported"
	if withMarkers {
		cell = "✅ Supported"
	}
	return map[string]any{
		"title": "T",
		"blocks": []any{
			map[string]any{"type": "table", "rows": []any{
				[]any{"Feature", "Value"},
				[]any{"Row", cell},
			}},
		},
	}
}

func TestPDFEmbedsEmojiForMarkers(t *testing.T) {
	gen := func(withMarkers bool) []byte {
		var buf bytes.Buffer
		if _, err := (PDFGenerator{}).Generate(GenerateRequest{Filename: "m", Payload: markerPayload(withMarkers)}, &buf); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return buf.Bytes()
	}
	withImg := bytes.Contains(gen(true), []byte("/Subtype /Image"))
	withoutImg := bytes.Contains(gen(false), []byte("/Subtype /Image"))
	if !withImg {
		t.Error("marker cell did not embed an emoji image")
	}
	if withoutImg {
		t.Error("marker-free document unexpectedly embedded an image")
	}
}

// Gated end-to-end render for manual visual inspection.
//
//	OUT_DIR=/tmp go test ./internal/docgen -run TestGenerateMarkerPDF -v
func TestGenerateMarkerPDF(t *testing.T) {
	outDir := os.Getenv("OUT_DIR")
	if outDir == "" {
		t.Skip("set OUT_DIR to write the verification PDF")
	}
	payload := map[string]any{
		"title":    "Bifrost vs LiteLLM: Gated Features Comparison",
		"subtitle": "Enterprise-only features for AI gateway procurement evaluation",
		"blocks": []any{
			map[string]any{"type": "paragraph", "text": "The ✓ marks below show where each product delivers natively."},
			map[string]any{"type": "table", "rows": []any{
				[]any{"Feature", "Bifrost Enterprise", "LiteLLM Enterprise"},
				[]any{"SSO / SAML / OIDC", "✅️ SAML SSO, RBAC, and policy enforcement across every team and workspace in the org", "✓ SSO, SCIM"},
				[]any{"MCP gateway", "✔ Supported", "❌ Not available"},
				[]any{"Adaptive load balancing", "✓", "✗"},
			}},
			map[string]any{"type": "bullets", "items": []any{
				"✓ Included in every tier at no additional cost for the whole account",
				"✗ Not available without an Enterprise contract",
				"Plain bullet with no marker",
			}},
		},
	}
	var buf bytes.Buffer
	if _, err := (PDFGenerator{}).Generate(GenerateRequest{Filename: "marker-verify", Payload: payload}, &buf); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := filepath.Join(outDir, "marker-verify.pdf")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s (%d bytes)", out, buf.Len())
}
