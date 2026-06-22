package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
)

func TestListArtifactsReturnsCurrentUsersArtifacts(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{
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
	server := newAuthenticatedServer(t, Deps{
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
	server := newAuthenticatedServer(t, Deps{
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
	server := newAuthenticatedServer(t, Deps{
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

func TestDeleteArtifactRemovesOwnArtifactRowAndFile(t *testing.T) {
	// Canonicalize the temp dir: on macOS t.TempDir() sits under /var (a symlink to
	// /private/var), and ResolveExisting EvalSymlinks the parent, so an
	// unresolved usersDir would fail its inside-root check and skip file removal.
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Mirror the real layout: artifacts live under files/outputs, not directly in
	// the user root (ResolveExisting rejects a file whose parent is the root).
	relPath := "files/outputs/art_1.png"
	absFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted := []string{}
	store := fakeArtifactStore{
		deleted: &deleted,
		artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png"},
		},
	}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/artifacts/art_1", ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(absFile); !os.IsNotExist(err) {
		t.Fatalf("artifact file still exists, err = %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "art_1" {
		t.Fatalf("deleted rows = %v, want [art_1]", deleted)
	}
}

func TestDeleteArtifactRejectsAnotherUsersArtifact(t *testing.T) {
	deleted := []string{}
	store := fakeArtifactStore{
		deleted: &deleted,
		artifacts: []artifact.Artifact{
			{ID: "art_2", UserID: "user_2", VolumeRelPath: "art_2.png", DisplayFilename: "b.png"},
		},
	}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/artifacts/art_2", ""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want 404 for another user's artifact", rec.Code, rec.Body.String())
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none for a foreign artifact", deleted)
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
