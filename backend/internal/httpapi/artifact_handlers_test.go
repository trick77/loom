package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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

func TestRenameArtifactUpdatesNameForOwner(t *testing.T) {
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", DisplayFilename: "old.md", MIMEType: "text/markdown"},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodPatch, "/api/artifacts/art_1", `{"displayFilename":"new.md"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got artifactResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DisplayFilename != "new.md" {
		t.Fatalf("DisplayFilename = %q, want new.md", got.DisplayFilename)
	}
}

func TestRenameArtifactLocksOriginalExtension(t *testing.T) {
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", DisplayFilename: "report.md", MIMEType: "text/markdown"},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	// A direct PATCH trying to change the extension is forced back to the original.
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodPatch, "/api/artifacts/art_1", `{"displayFilename":"evil.exe"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got artifactResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DisplayFilename != "evil.md" {
		t.Fatalf("DisplayFilename = %q, want evil.md (extension locked)", got.DisplayFilename)
	}
}

func TestRenameArtifactRejectsBadInput(t *testing.T) {
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", DisplayFilename: "a.md"},
		{ID: "art_2", UserID: "user_2", DisplayFilename: "b.md"},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	cases := []struct {
		name   string
		id     string
		body   string
		status int
	}{
		{"empty name", "art_1", `{"displayFilename":"   "}`, http.StatusBadRequest},
		{"traversal", "art_1", `{"displayFilename":"../../etc/passwd"}`, http.StatusBadRequest},
		{"another user", "art_2", `{"displayFilename":"hijack.md"}`, http.StatusNotFound},
		{"unknown id", "art_x", `{"displayFilename":"x.md"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, authenticatedRequest(http.MethodPatch, "/api/artifacts/"+tc.id, tc.body))
			if rec.Code != tc.status {
				t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), tc.status)
			}
		})
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

func pngFileBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestThumbnailArtifactGeneratesServesAndBackfills(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	absFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absFile, pngFileBytes(t, 400, 300), 0o644); err != nil {
		t.Fatal(err)
	}
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 1234},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type = %q, want image/jpeg", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != artifactCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cc, artifactCacheControl)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("ETag header missing")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode thumbnail body: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("thumbnail format = %q, want jpeg", format)
	}
	longest := cfg.Width
	if cfg.Height > longest {
		longest = cfg.Height
	}
	if longest > artifact.ThumbnailMaxDimension {
		t.Fatalf("thumbnail longest side = %d, want <= %d", longest, artifact.ThumbnailMaxDimension)
	}
	// The sidecar file was written next to the original...
	if _, err := os.Stat(absFile + artifact.ThumbnailSuffix); err != nil {
		t.Fatalf("thumbnail sidecar not written: %v", err)
	}
	// ...and the relpath was backfilled onto the row for future requests.
	if got := store.artifacts[0].ThumbnailRelPath; got != relPath+artifact.ThumbnailSuffix {
		t.Fatalf("ThumbnailRelPath = %q, want %q (lazy backfill persisted)", got, relPath+artifact.ThumbnailSuffix)
	}
}

func TestThumbnailArtifactNonImageReturns404(t *testing.T) {
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "doc_1", UserID: "user_1", VolumeRelPath: "files/outputs/doc.txt", DisplayFilename: "doc.txt", MIMEType: "text/plain; charset=utf-8"},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/doc_1/thumbnail", ""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a non-image artifact; body=%s", rec.Code, rec.Body.String())
	}
}

func TestThumbnailArtifactSvgReturns404(t *testing.T) {
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "svg_1", UserID: "user_1", VolumeRelPath: "files/outputs/diagram.svg", DisplayFilename: "diagram.svg", MIMEType: "image/svg+xml; charset=utf-8"},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: t.TempDir()})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/svg_1/thumbnail", ""))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an SVG (served as its own preview, no raster thumbnail); body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadArtifactSetsImmutableCacheHeaders(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	absFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absFile, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := fakeArtifactStore{artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 9},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/download", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != artifactCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cc, artifactCacheControl)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag header missing")
	}
	// A conditional revisit with the same validator is a 304, not a re-download.
	condReq := authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/download", "")
	condReq.Header.Set("If-None-Match", etag)
	condRec := httptest.NewRecorder()
	server.ServeHTTP(condRec, condReq)
	if condRec.Code != http.StatusNotModified {
		t.Fatalf("conditional GET status = %d, want 304", condRec.Code)
	}
}

func TestDeleteArtifactRemovesThumbnailSidecar(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	absFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	thumbFile := absFile + artifact.ThumbnailSuffix
	if err := os.WriteFile(thumbFile, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	deleted := []string{}
	store := fakeArtifactStore{
		deleted: &deleted,
		artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", ThumbnailRelPath: relPath + artifact.ThumbnailSuffix},
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
	if _, err := os.Stat(thumbFile); !os.IsNotExist(err) {
		t.Fatalf("thumbnail sidecar still exists, err = %v", err)
	}
}
