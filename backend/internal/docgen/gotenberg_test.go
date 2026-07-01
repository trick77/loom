package docgen

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGotenbergConvert_PostsMultipart(t *testing.T) {
	var gotPath, gotHTML, gotPrintBg, gotContentType string
	var sawFont bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		gotPrintBg = r.FormValue("printBackground")
		for _, fh := range r.MultipartForm.File["files"] {
			if fh.Filename == "index.html" {
				f, _ := fh.Open()
				b, _ := io.ReadAll(f)
				gotHTML = string(b)
			}
			if strings.HasSuffix(fh.Filename, ".ttf") {
				sawFont = true
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("%PDF-1.7 mock"))
	}))
	defer srv.Close()

	c := NewGotenbergClient(GotenbergConfig{BaseURL: srv.URL})
	pdf, err := c.Convert(context.Background(), "<html>hi</html>",
		[]gotenbergAsset{{Filename: "Go-Mono.ttf", Data: []byte("font")}}, defaultConvertOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if string(pdf) != "%PDF-1.7 mock" {
		t.Errorf("pdf = %q", pdf)
	}
	if gotPath != "/forms/chromium/convert/html" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("content-type = %q", gotContentType)
	}
	if gotHTML != "<html>hi</html>" {
		t.Errorf("index.html body = %q", gotHTML)
	}
	if gotPrintBg != "true" {
		t.Errorf("printBackground = %q, want true", gotPrintBg)
	}
	if !sawFont {
		t.Error("font asset was not uploaded")
	}
}

func TestGotenbergConvert_MapsErrors(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		wantSub string
	}{
		{http.StatusBadRequest, "bad html here", "400"},
		{http.StatusConflict, "", "unavailable (409)"},
		{http.StatusServiceUnavailable, "", "unavailable (503)"},
		{http.StatusInternalServerError, "", "status 500"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c := NewGotenbergClient(GotenbergConfig{BaseURL: srv.URL})
		_, err := c.Convert(context.Background(), "<html></html>", nil, defaultConvertOptions())
		if err == nil {
			t.Errorf("status %d: expected error", tc.status)
		} else if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("status %d: error %q missing %q", tc.status, err.Error(), tc.wantSub)
		}
		srv.Close()
	}
	// 400 body should be surfaced.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("malformed markup"))
	}))
	defer srv.Close()
	_, err := NewGotenbergClient(GotenbergConfig{BaseURL: srv.URL}).Convert(context.Background(), "x", nil, defaultConvertOptions())
	if err == nil || !strings.Contains(err.Error(), "malformed markup") {
		t.Errorf("400 body not surfaced: %v", err)
	}
}

func TestGotenbergConvert_TransportError(t *testing.T) {
	// Nothing is listening here.
	c := NewGotenbergClient(GotenbergConfig{BaseURL: "http://127.0.0.1:0"})
	if _, err := c.Convert(context.Background(), "x", nil, defaultConvertOptions()); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestGotenbergConvert_HonorsCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewGotenbergClient(GotenbergConfig{BaseURL: srv.URL}).Convert(ctx, "x", nil, defaultConvertOptions()); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
