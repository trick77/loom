package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/documents"
	"github.com/trick77/slopr/internal/rag"
)

type fakeDocumentService struct {
	uploaded  documents.UploadInput
	uploadErr error
	doc       rag.Document
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
func (f *fakeDocumentService) Retrieve(context.Context, string, *string, string, int) ([]rag.RetrievedChunk, error) {
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

func TestHandleUploadDocument_disabledWhenServiceNil(t *testing.T) {
	server := newAuthenticatedChatServer(t, Deps{})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, multipartUpload(t, "a.pdf", "bytes", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when documents disabled", rec.Code)
	}
}
