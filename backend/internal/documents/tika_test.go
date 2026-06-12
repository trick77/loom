package documents

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTikaClient_Extract_returnsPlainText(t *testing.T) {
	var gotMethod, gotAccept, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		io.WriteString(w, "Hello extracted world\n")
	}))
	defer srv.Close()

	c := NewTikaClient(TikaConfig{BaseURL: srv.URL})
	text, err := c.Extract(context.Background(), "report.pdf", "application/pdf", strings.NewReader("RAWPDFBYTES"))
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	if want := "Hello extracted world"; strings.TrimSpace(text) != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/tika" {
		t.Errorf("path = %s, want /tika", gotPath)
	}
	if !strings.HasPrefix(gotAccept, "text/plain") {
		t.Errorf("Accept = %q, want text/plain", gotAccept)
	}
	if gotBody != "RAWPDFBYTES" {
		t.Errorf("forwarded body = %q, want the raw file bytes", gotBody)
	}
}

func TestTikaClient_Extract_errorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := NewTikaClient(TikaConfig{BaseURL: srv.URL})
	if _, err := c.Extract(context.Background(), "x.pdf", "application/pdf", strings.NewReader("x")); err == nil {
		t.Fatal("Extract() error = nil, want error on 422")
	}
}

func TestTikaClient_Extract_capsExtractedText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("a", maxExtractedTextBytes+5000))
	}))
	defer srv.Close()

	c := NewTikaClient(TikaConfig{BaseURL: srv.URL})
	text, err := c.Extract(context.Background(), "big.txt", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if len(text) > maxExtractedTextBytes {
		t.Errorf("extracted text len = %d, want <= %d (capped)", len(text), maxExtractedTextBytes)
	}
}
