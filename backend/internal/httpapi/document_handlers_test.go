package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/documents"
	"github.com/trick77/slopr/internal/rag"
)

type fakeDocumentService struct {
	uploaded           documents.UploadInput
	uploadErr          error
	doc                rag.Document
	deletedThreadData  []string
	deletedProjectData []string
	deleteDataErr      error
}

func (f *fakeDocumentService) Upload(_ context.Context, in documents.UploadInput) (rag.Document, artifact.Artifact, error) {
	f.uploaded = in
	if f.uploadErr != nil {
		return rag.Document{}, artifact.Artifact{}, f.uploadErr
	}
	return f.doc, artifact.Artifact{}, nil
}
func (f *fakeDocumentService) List(context.Context, string, *string) ([]rag.Document, error) {
	return []rag.Document{f.doc}, nil
}
func (f *fakeDocumentService) Get(context.Context, string, string) (rag.Document, bool, error) {
	return f.doc, true, nil
}
func (f *fakeDocumentService) Index(context.Context, string, string) error   { return nil }
func (f *fakeDocumentService) Unindex(context.Context, string, string) error { return nil }
func (f *fakeDocumentService) Delete(context.Context, string, string) error  { return nil }
func (f *fakeDocumentService) DeleteThreadData(_ context.Context, _ string, threadID string) error {
	f.deletedThreadData = append(f.deletedThreadData, threadID)
	return f.deleteDataErr
}
func (f *fakeDocumentService) DeleteProjectData(_ context.Context, _ string, projectID string) error {
	f.deletedProjectData = append(f.deletedProjectData, projectID)
	return f.deleteDataErr
}
func (f *fakeDocumentService) Retrieve(context.Context, string, *string, *string, string, int) ([]rag.RetrievedChunk, error) {
	return nil, nil
}

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
	svc := &fakeDocumentService{doc: rag.Document{ID: "d1", Filename: "a.pdf", Status: rag.StatusPending}}
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.pdf", "bytes", map[string]string{"projectId": "p1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if svc.uploaded.Filename != "a.pdf" {
		t.Errorf("forwarded filename = %q, want a.pdf", svc.uploaded.Filename)
	}
	if svc.uploaded.ProjectID == nil || *svc.uploaded.ProjectID != "p1" {
		t.Errorf("forwarded projectId = %v, want p1", svc.uploaded.ProjectID)
	}
}

func TestHandleUploadDocument_unsupportedFormat(t *testing.T) {
	svc := &fakeDocumentService{uploadErr: documents.ErrUnsupportedFormat}
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "x.exe", "bytes", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestHandleUploadDocument_chatDocumentLimit(t *testing.T) {
	svc := &fakeDocumentService{uploadErr: documents.ErrChatDocumentLimit}
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})

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
	svc := &fakeDocumentService{uploadErr: documents.ErrTooLarge}
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})

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
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})
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
	server := newAuthenticatedChatServer(t, Deps{Documents: svc})
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
	server := newAuthenticatedChatServer(t, Deps{})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.pdf", "bytes", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when documents disabled", rec.Code)
	}
}
