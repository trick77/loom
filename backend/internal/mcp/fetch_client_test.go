package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFetchClientAdvertisesFetchTool(t *testing.T) {
	client := NewFetchClient("fetch", nil)
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "fetch__fetch" {
		t.Fatalf("exposed name = %q, want fetch__fetch", tool.Name)
	}
	if tool.OriginalName != "fetch" {
		t.Fatalf("original name = %q, want fetch", tool.OriginalName)
	}
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema properties missing: %#v", tool.InputSchema)
	}
	for _, key := range []string{"url", "max_length", "start_index", "raw", "include_metadata", "full_page", "selector", "exclude_selectors"} {
		if _, ok := props[key]; !ok {
			t.Errorf("InputSchema missing property %q", key)
		}
	}
	if _, ok := props["extract_pdf"]; ok {
		t.Error("InputSchema must not expose extract_pdf")
	}
}

func TestFetchClientCallToolRequiresURL(t *testing.T) {
	client := NewFetchClient("fetch", nil)
	// An empty URL fails before any network access, and the error must be
	// non-nil so the deterministic fetch->obscura fallback fires.
	_, err := client.CallTool(context.Background(), "fetch", map[string]any{})
	if err == nil {
		t.Fatal("CallTool with no url should error")
	}
	if !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchClientOptionsWirePDFExtractor(t *testing.T) {
	if (NewFetchClient("fetch", nil).(*fetchClient)).options(nil).PDFHandler != nil {
		t.Fatal("no extractor must leave PDFHandler nil")
	}
	extract := func(_ context.Context, body []byte) (string, error) { return "text:" + string(body), nil }
	opts := (NewFetchClient("fetch", extract).(*fetchClient)).options(map[string]any{"raw": true})
	if !opts.Raw {
		t.Fatal("tool arguments must still map onto options")
	}
	got, err := opts.PDFHandler(context.Background(), []byte("%PDF-"))
	if err != nil || got != "text:%PDF-" {
		t.Fatalf("PDFHandler = %q, %v; want the extractor's output", got, err)
	}
}

func TestFetchClientDescriptionMentionsPDFsOnlyWithExtractor(t *testing.T) {
	extract := func(context.Context, []byte) (string, error) { return "", nil }
	if strings.Contains((NewFetchClient("fetch", nil).(*fetchClient)).description(), "PDF") {
		t.Fatal("without an extractor the description must not promise PDF text")
	}
	if !strings.Contains((NewFetchClient("fetch", extract).(*fetchClient)).description(), "PDFs are returned as extracted text") {
		t.Fatal("with an extractor the description must say PDFs are extracted")
	}
}

func TestIsPDFExtractionError(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", PDFExtractionError{Err: errors.New("Failed to extract PDF x")})
	if !IsPDFExtractionError(err) || err.Error() != "wrapped: Failed to extract PDF x" {
		t.Fatalf("IsPDFExtractionError(%v) = false, or the message changed", err)
	}
	if IsPDFExtractionError(errors.New("Failed to fetch x")) {
		t.Fatal("a plain fetch error is not a PDF extraction error")
	}
}

func TestFetchClientCloseIsNil(t *testing.T) {
	if err := NewFetchClient("fetch", nil).Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
