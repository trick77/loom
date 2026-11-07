package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/trick77/loom/internal/artifact"
)

func blobBlock(uri, mimeType string, data []byte) map[string]any {
	return map[string]any{"type": "resource", "resource": map[string]any{"uri": uri, "mimeType": mimeType, "blob": base64.StdEncoding.EncodeToString(data)}}
}

func blobContent(uri, mimeType string, data []byte) toolContent {
	return toolContent{Type: "resource", Resource: &toolResource{URI: uri, MimeType: mimeType, Blob: base64.StdEncoding.EncodeToString(data)}}
}

func pdfBytes(size int) []byte {
	data := make([]byte, size)
	copy(data, "%PDF-1.7\n")
	return data
}

// A document comes through only from a server allowed to make artifacts, and
// a text resource beside it stays run_python's file.
func TestToolDocumentsOnlyFromArtifactServers(t *testing.T) {
	server, _ := newModernServer(t, func(modernRequest) any {
		return map[string]any{
			"resultType": "complete",
			"content": []any{
				map[string]any{"type": "text", "text": ""},
				map[string]any{"type": "resource", "resource": map[string]any{"uri": "file:///rows.csv", "mimeType": "text/csv", "text": "a,b\n"}},
				blobBlock("file:///Q3 report.pdf", "application/pdf", pdfBytes(64)),
			},
		}
	})
	plain := NewRemoteClient("docs", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, nil)
	got, err := plain.CallTool(context.Background(), "create", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 0 || got.DroppedDocuments != 0 || len(got.Files) != 1 {
		t.Fatalf("server without artifacts: %+v", got)
	}
	trusted := NewRemoteClient("docs", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL, Artifacts: true}, nil)
	got, err = trusted.CallTool(context.Background(), "create", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Documents) != 1 || len(got.Files) != 1 || got.Files[0].Name != "rows.csv" {
		t.Fatalf("server with artifacts: %+v", got)
	}
	doc := got.Documents[0]
	if doc.Name != "Q3 report.pdf" || doc.Extension != "pdf" || !bytes.Equal(doc.Data, pdfBytes(64)) {
		t.Fatalf("document %+v", doc)
	}
}

func TestToolDocumentsAcceptTheGeneratedTypes(t *testing.T) {
	zip := []byte("PK\x03\x04rest")
	for _, tc := range []struct {
		uri, mimeType string
		data          []byte
		ext           string
	}{
		{"file:///a.pdf", "application/pdf", pdfBytes(16), "pdf"},
		{"file:///a.docx", artifact.MIMEType("docx"), zip, "docx"},
		{"file:///a.XLSX", artifact.MIMEType("xlsx"), zip, "xlsx"},
		{"file:///a.pptx", artifact.MIMEType("pptx"), zip, "pptx"},
		{"file:///notes.md", "text/markdown; charset=utf-8", []byte("# Grüezi"), "md"},
		{"file:///notes.md", "text/markdown", []byte("# x"), "md"},
		{"file:///rows.csv", "text/csv", []byte("a,b"), "csv"},
		{"file:///page.html", "text/html; charset=utf-8", []byte("<p>x</p>"), "html"},
		{"file:///cfg.yaml", "text/plain; charset=utf-8", []byte("a: 1"), "yaml"},
		{"file:///a.txt", "TEXT/PLAIN", []byte("x"), "txt"},
		{"/out/dir/a.json", "application/json", []byte("{}"), "json"},
	} {
		kept := toolArtifacts([]toolContent{blobContent(tc.uri, tc.mimeType, tc.data)}, nil, 1024)
		if len(kept.documents) != 1 || kept.droppedDocuments != 0 || kept.documents[0].Extension != tc.ext {
			t.Errorf("%s (%s): %+v", tc.uri, tc.mimeType, kept)
		}
	}
}

func TestToolDocumentsRejectUnsafeOrBroken(t *testing.T) {
	for name, item := range map[string]toolContent{
		"html under a pdf name":   blobContent("file:///a.pdf", "application/pdf", []byte("<html>")),
		"pdf type, html name":     blobContent("file:///a.html", "application/pdf", pdfBytes(16)),
		"no extension":            blobContent("file:///report", "application/pdf", pdfBytes(16)),
		"unknown type":            blobContent("file:///run.exe", "application/octet-stream", []byte("MZ")),
		"svg":                     blobContent("file:///a.svg", "image/svg+xml", []byte("<svg/>")),
		"binary as text":          blobContent("file:///a.txt", "text/plain", []byte("a\x00b")),
		"not utf-8":               blobContent("file:///a.csv", "text/csv", []byte{0xff, 0xfe}),
		"docx without zip":        blobContent("file:///a.docx", artifact.MIMEType("docx"), []byte("plain")),
		"over the size limit":     blobContent("file:///a.pdf", "application/pdf", pdfBytes(2048)),
		"not base64":              {Type: "resource", Resource: &toolResource{URI: "file:///a.pdf", MimeType: "application/pdf", Blob: "%%%"}},
		"directory name":          blobContent("file:///", "application/pdf", pdfBytes(16)),
		"markdown typed as plain": blobContent("file:///a.md", "text/plain", []byte("x")),
	} {
		kept := toolArtifacts([]toolContent{item}, nil, 1024)
		if len(kept.documents) != 0 || kept.droppedDocuments != 1 {
			t.Errorf("%s: %+v", name, kept)
		}
	}
}

// Images and documents of a reply share one size limit and one count, in
// content order.
func TestToolArtifactsShareTheLimits(t *testing.T) {
	img := toolContent{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(pngBytes(600))}
	doc := blobContent("file:///a.pdf", "application/pdf", pdfBytes(600))
	kept := toolArtifacts([]toolContent{img, doc}, nil, 1024)
	if len(kept.images) != 1 || len(kept.documents) != 0 || kept.droppedDocuments != 1 {
		t.Fatalf("size: %+v", kept)
	}
	small := blobContent("file:///a.pdf", "application/pdf", pdfBytes(16))
	var content []toolContent
	for range maxToolArtifacts + 1 {
		content = append(content, small)
	}
	kept = toolArtifacts(content, nil, 1024)
	if len(kept.documents) != maxToolArtifacts || kept.droppedDocuments != 1 {
		t.Fatalf("count: %+v", kept)
	}
}

func TestMediaType(t *testing.T) {
	for in, want := range map[string]string{"Text/Plain; charset=utf-8": "text/plain", "application/pdf": "application/pdf", " bad;; ": "bad;;"} {
		if got := mediaType(in); got != want {
			t.Errorf("mediaType(%q) = %q, want %q", in, got, want)
		}
	}
}
