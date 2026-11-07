package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// pngBytes is a PNG signature plus padding: enough for content sniffing.
func pngBytes(size int) []byte {
	data := make([]byte, size)
	copy(data, "\x89PNG\r\n\x1a\n")
	return data
}

func imageBlock(mimeType string, data []byte) map[string]any {
	return map[string]any{"type": "image", "mimeType": mimeType, "data": base64.StdEncoding.EncodeToString(data)}
}

func imageServer(t *testing.T, data []byte) string {
	t.Helper()
	server, _ := newModernServer(t, func(modernRequest) any {
		return map[string]any{
			"resultType": "complete",
			"content":    []any{map[string]any{"type": "text", "text": "done"}, imageBlock("image/png", data)},
			"structuredContent": map[string]any{"images": []map[string]any{{
				"filename": "cat", "model": "m1", "provider": "p1", "width": 640, "height": 480, "duration_ms": 1234,
			}}},
		}
	})
	return server.URL
}

func TestToolImagesOnlyFromArtifactServers(t *testing.T) {
	url := imageServer(t, pngBytes(64))
	plain := NewRemoteClient("img", ServerConfig{Transport: TransportStreamableHTTP, URL: url}, nil)
	got, err := plain.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got.Text != "done" || len(got.Images) != 0 {
		t.Fatalf("server without artifacts: %#v, want text only", got)
	}

	trusted := NewRemoteClient("img", ServerConfig{Transport: TransportStreamableHTTP, URL: url, Artifacts: true}, nil)
	got, err = trusted.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got.Text != "done" || len(got.Images) != 1 {
		t.Fatalf("server with artifacts: %#v, want one image", got)
	}
	img := got.Images[0]
	if img.MimeType != "image/png" || img.Extension() != "png" || !bytes.Equal(img.Data, pngBytes(64)) ||
		img.Filename != "cat" || img.Model != "m1" || img.Provider != "p1" || img.Width != 640 || img.Height != 480 || img.DurationMs != 1234 {
		t.Fatalf("image = %+v", img)
	}
}

// A picture outgrows the 4 MiB every other server is held to; only a server
// trusted with artifacts may answer that large.
func TestToolImagesLargeResponseOnlyForArtifactServers(t *testing.T) {
	url := imageServer(t, pngBytes(6<<20))
	plain := NewRemoteClient("img", ServerConfig{Transport: TransportStreamableHTTP, URL: url}, nil)
	if _, err := plain.CallTool(context.Background(), "search", nil); err == nil {
		t.Fatal("server without artifacts: 8 MiB response accepted, want the 4 MiB cap")
	}
	trusted := NewRemoteClient("img", ServerConfig{Transport: TransportStreamableHTTP, URL: url, Artifacts: true}, nil)
	got, err := trusted.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("server with artifacts: %v", err)
	}
	if len(got.Images) != 1 || len(got.Images[0].Data) != 6<<20 {
		t.Fatalf("images = %d", len(got.Images))
	}
}

func TestToolImagesRejectUnsafeOrBroken(t *testing.T) {
	html := []byte("<!doctype html><script>alert(1)</script>")
	cases := map[string]toolContent{
		"svg is not a raster":        {Type: "image", MimeType: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte("<svg/>"))},
		"bytes disagree with type":   {Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(html)},
		"not base64":                 {Type: "image", MimeType: "image/png", Data: "%%%"},
		"empty":                      {Type: "image", MimeType: "image/png"},
		"over the size limit":        {Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(pngBytes(2048))},
		"jpeg bytes labelled as png": {Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"))},
	}
	for name, item := range cases {
		if got := toolImages([]toolContent{item}, nil, 1024); len(got) != 0 {
			t.Errorf("%s: kept %+v", name, got)
		}
	}
}

func TestToolImagesCappedAndMetaByPosition(t *testing.T) {
	var content []toolContent
	var meta []imageMeta
	for i := range maxToolImages + 2 {
		content = append(content, toolContent{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(pngBytes(32))})
		meta = append(meta, imageMeta{Filename: string(rune('a' + i))})
	}
	// A dropped block keeps its slot: meta[i] belongs to the i-th image block.
	content[0].MimeType = "image/svg+xml"
	got := toolImages(content, meta, 1024)
	if len(got) != maxToolImages {
		t.Fatalf("%d images, want %d", len(got), maxToolImages)
	}
	if got[0].Filename != "b" {
		t.Errorf("first kept image named %q, want b (meta by block position)", got[0].Filename)
	}
}

func TestToolImagesNeverJoinText(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"content": []any{
		map[string]any{"type": "text", "text": "a"},
		imageBlock("image/png", pngBytes(32)),
	}})
	var r callToolResult
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if got := r.toCallResult(true); got.Text != "a" || len(got.Images) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadServersFromFileArtifactsOptIn(t *testing.T) {
	path := writeTempFile(t, `{"mcpServers":{
		"img":{"type":"http","url":"http://img:8080/mcp","artifacts":true},
		"web":{"type":"http","url":"http://web:8080/mcp"}
	}}`)
	servers, err := LoadServersFromFile(path, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !servers["img"].Artifacts || servers["web"].Artifacts {
		t.Fatalf("artifacts img=%v web=%v, want only the opted-in server", servers["img"].Artifacts, servers["web"].Artifacts)
	}
}
