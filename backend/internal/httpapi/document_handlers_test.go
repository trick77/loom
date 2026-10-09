package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/rag"
)

func multipartUpload(t *testing.T, filename, content string, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte(content))
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	return req
}

func TestHandleUploadDocument_success(t *testing.T) {
	svc := &fakeDocumentService{Doc: rag.Document{ID: "d1", Filename: "a.pdf", Status: rag.StatusPending}}
	server := newAuthenticatedServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.pdf", "bytes", map[string]string{"projectId": "p1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if svc.Uploaded.Filename != "a.pdf" {
		t.Errorf("forwarded filename = %q, want a.pdf", svc.Uploaded.Filename)
	}
	if svc.Uploaded.ProjectID == nil || *svc.Uploaded.ProjectID != "p1" {
		t.Errorf("forwarded projectId = %v, want p1", svc.Uploaded.ProjectID)
	}
}

func TestHandleUploadDocument_unsupportedFormat(t *testing.T) {
	svc := &fakeDocumentService{UploadErr: documents.ErrUnsupportedFormat}
	server := newAuthenticatedServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "x.exe", "bytes", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestHandleUploadDocument_chatDocumentLimit(t *testing.T) {
	svc := &fakeDocumentService{UploadErr: documents.ErrThreadDocumentLimit}
	server := newAuthenticatedServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.txt", "bytes", map[string]string{"threadId": "t1"}))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUploadDocument_payloadTooLarge(t *testing.T) {
	// Content over the limit is enforced in documents.Upload (which returns
	// ErrTooLarge); the handler must map that to 413 so the client reports a size
	// error. The request body itself stays within the handler's MaxBytesReader.
	svc := &fakeDocumentService{UploadErr: documents.ErrTooLarge}
	server := newAuthenticatedServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "large.pdf", "bytes", map[string]string{"threadId": "t1"}))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUploadDocument_oversizedBodyRejectedByMaxBytes(t *testing.T) {
	// A body that exceeds even the multipart-overhead allowance is stopped at the
	// handler's MaxBytesReader during parsing, before reaching the service.
	svc := &fakeDocumentService{}
	server := newAuthenticatedServer(t, Deps{Documents: svc})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "huge.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("x"), artifact.MaxArtifactSizeBytes+(2<<20))); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/documents/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUploadDocument_malformedBodyIsBadRequestNotTooLarge(t *testing.T) {
	svc := &fakeDocumentService{}
	server := newAuthenticatedServer(t, Deps{Documents: svc})
	// A multipart content type with a body that is not actually valid multipart:
	// the parse fails for a reason other than size, so it must not be reported as
	// a 413 (which the client renders as a "25 MB or smaller" size error).
	req := httptest.NewRequest(http.MethodPost, "/api/documents/upload", strings.NewReader("not a multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=does-not-match")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for malformed upload; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUploadDocument_disabledWhenServiceNil(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.pdf", "bytes", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when documents disabled", rec.Code)
	}
}

func TestToDocumentResponse_setsDownloadURLFromArtifact(t *testing.T) {
	artID := "art_42"
	got := toDocumentResponse(rag.Document{ID: "d1", Filename: "chart.png", MIME: "image/png", ArtifactID: &artID})
	if got.DownloadURL != "/api/artifacts/art_42/download" {
		t.Errorf("DownloadURL = %q, want /api/artifacts/art_42/download", got.DownloadURL)
	}
	noArt := toDocumentResponse(rag.Document{ID: "d2", Filename: "notes.md"})
	if noArt.DownloadURL != "" {
		t.Errorf("DownloadURL = %q, want empty when no artifact", noArt.DownloadURL)
	}
}

// A delete the service refuses while an ingest runs answers 409 so the client
// keeps polling.
func TestHandleDeleteDocument_conflictWhileIndexing(t *testing.T) {
	svc := &fakeDocumentService{
		Doc:       rag.Document{ID: "d1", Filename: "a.pdf", Status: rag.StatusExtracting},
		DeleteErr: documents.ErrIndexInProgress,
	}
	server := newAuthenticatedServer(t, Deps{Documents: svc})
	for _, req := range []*http.Request{
		authenticatedRequest(http.MethodDelete, "/api/documents/d1", ""),
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s %s status = %d, want 409; body=%s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
		}
	}
}

// The ingest runs in the background group, not on a bare goroutine: a panic
// in it is recovered and shutdown drains it before the database closes.
func TestHandleIndexDocument_runsIngestInBackgroundGroup(t *testing.T) {
	svc := &fakeDocumentService{
		Doc:        rag.Document{ID: "d1", Filename: "a.pdf", Status: rag.StatusPending},
		IndexCalls: make(chan string, 1),
	}
	bg := background.New(context.Background())
	server := newAuthenticatedServer(t, Deps{Documents: svc, Background: bg})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/documents/d1/index", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if err := bg.Stop(time.Second); err != nil {
		t.Fatalf("Stop() error = %v (the ingest was not tracked by the group)", err)
	}
	select {
	case id := <-svc.IndexCalls:
		if id != "d1" {
			t.Fatalf("indexed %q, want d1", id)
		}
	default:
		t.Fatal("Index was not run")
	}
}
