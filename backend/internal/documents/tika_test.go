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

func TestTikaClient_Ping(t *testing.T) {
	t.Run("healthy returns nil", func(t *testing.T) {
		var gotMethod, gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			io.WriteString(w, "Apache Tika 2.9.2.1")
		}))
		defer srv.Close()
		if err := NewTikaClient(TikaConfig{BaseURL: srv.URL}).Ping(context.Background()); err != nil {
			t.Fatalf("Ping() error: %v", err)
		}
		if gotMethod != http.MethodGet {
			t.Errorf("method = %s, want GET", gotMethod)
		}
		if gotPath != "/version" {
			t.Errorf("path = %s, want /version", gotPath)
		}
	})

	t.Run("non-2xx is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		if err := NewTikaClient(TikaConfig{BaseURL: srv.URL}).Ping(context.Background()); err == nil {
			t.Fatal("Ping() error = nil, want error on 503")
		}
	})

	t.Run("unreachable is an error", func(t *testing.T) {
		if err := NewTikaClient(TikaConfig{BaseURL: "http://127.0.0.1:0"}).Ping(context.Background()); err == nil {
			t.Fatal("Ping() error = nil, want transport error")
		}
	})
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
