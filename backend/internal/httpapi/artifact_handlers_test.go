package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
)

func TestListArtifactsReturnsCurrentUsersArtifacts(t *testing.T) {
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{
				ID:              "art_1",
				UserID:          "user_1",
				ThreadID:        "thread_1",
				DisplayFilename: "robot.png",
				MIMEType:        "image/png",
				SizeBytes:       842000,
				Source:          "assistant_generated",
				CreatedAt:       time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC),
				DownloadURL:     "/api/artifacts/art_1/download",
			},
			{
				ID:              "art_2",
				UserID:          "user_2",
				ThreadID:        "thread_2",
				DisplayFilename: "other.png",
				MIMEType:        "image/png",
				SizeBytes:       10,
				Source:          "uploaded",
				CreatedAt:       time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC),
				DownloadURL:     "/api/artifacts/art_2/download",
			},
		}},
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts?type=images&sort=modified&order=desc&search=robot", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got artifactListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(response items) = %d, want 1: %#v", len(got.Items), got)
	}
	if got.Items[0].ID != "art_1" || got.Items[0].DisplayFilename != "robot.png" || got.Items[0].ModifiedAt.IsZero() {
		t.Fatalf("response item = %#v", got.Items[0])
	}
	if got.NextCursor != nil {
		t.Fatalf("nextCursor = %q, want nil for a partial page", *got.NextCursor)
	}
}

func TestUploadImageAttachmentEnforcesThreadImageLimit(t *testing.T) {
	items := make([]artifact.Artifact, 0, 10)
	for i := 0; i < 10; i++ {
		items = append(items, artifact.Artifact{
			ID:              "art_limit",
			UserID:          "user_1",
			ThreadID:        "thread_1",
			DisplayFilename: "image.png",
			MIMEType:        "image/png",
			Source:          "user_uploaded",
		})
	}
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: fakeArtifactStore{artifacts: items},
	})

	body, contentType := multipartUploadBody(t, "file", "next.png", "image/png", []byte("png"))
	req := authenticatedRequest(http.MethodPost, "/api/artifacts/images/upload", body.String())
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadImageAttachmentReturnsPayloadTooLarge(t *testing.T) {
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})

	body, contentType := multipartUploadBody(t, "file", "large.png", "image/png", bytes.Repeat([]byte("x"), artifact.MaxArtifactSizeBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/images/upload", body)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestUploadImageAttachmentAllowsMaxSizeFileWithMultipartOverhead(t *testing.T) {
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})

	body, contentType := multipartUploadBody(t, "file", "limit.png", "image/png", bytes.Repeat([]byte("x"), artifact.MaxArtifactSizeBytes))
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/images/upload", body)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for max-size file upload with multipart overhead; body=%s", rec.Code, rec.Body.String())
	}
}

func multipartUploadBody(t *testing.T, field, filename, contentType string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + field + `"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.WriteField("threadId", "thread_1"); err != nil {
		t.Fatalf("write thread field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
