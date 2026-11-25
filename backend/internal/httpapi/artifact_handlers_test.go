package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
)

func TestListArtifactsReturnsCurrentUsersArtifacts(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{
		Artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{
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

func TestListArtifactsParsesPeriodAndReportsThreadTitle(t *testing.T) {
	var listed artifact.ListOptions
	server := newAuthenticatedServer(t, Deps{
		Artifacts: fakeArtifactStore{
			Artifacts: []artifact.Artifact{
				{ID: "art_1", UserID: "user_1", ThreadID: "thread_1", DisplayFilename: "a.png", MIMEType: "image/png"},
				{ID: "art_2", UserID: "user_1", DisplayFilename: "b.txt", MIMEType: "text/plain"},
			},
			ThreadTitles: map[string]string{"thread_1": "Quarterly planning"},
			Listed:       &listed,
		},
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet,
		"/api/artifacts?search=plan&since=2026-10-01T02%3A00%3A00%2B02%3A00&until=2026-11-01T00%3A00%3A00.000Z", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	wantSince := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wantUntil := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if !listed.Since.Equal(wantSince) || !listed.Until.Equal(wantUntil) || listed.Search != "plan" {
		t.Fatalf("listed options = %+v, want since %v until %v search plan", listed, wantSince, wantUntil)
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(raw.Items) != 2 {
		t.Fatalf("items = %#v, want 2", raw.Items)
	}
	if raw.Items[0]["threadTitle"] != "Quarterly planning" {
		t.Fatalf("threadTitle = %#v, want the thread's title", raw.Items[0]["threadTitle"])
	}
	if _, present := raw.Items[1]["threadTitle"]; present {
		t.Fatalf("thread-less item carries threadTitle: %#v", raw.Items[1])
	}

	// Absent bounds stay zero.
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts", ""))
	if rec.Code != http.StatusOK || !listed.Since.IsZero() || !listed.Until.IsZero() {
		t.Fatalf("status = %d listed = %+v, want 200 and open bounds", rec.Code, listed)
	}
}

func TestListArtifactsRejectsBadParams(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{}})
	for query, want := range map[string]string{
		"since=yesterday":  "invalid since",
		"since=2026-10-01": "invalid since",
		"until=nope":       "invalid until",
		"type=videos":      "invalid type",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts?"+query, ""))
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("%s: status = %d body = %s, want 400 %q", query, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestArtifactSummaryReturnsMonthsAndOlderCount(t *testing.T) {
	var summarized artifact.SummaryOptions
	server := newAuthenticatedServer(t, Deps{
		Artifacts: fakeArtifactStore{
			SummaryResult: artifact.Summary{OlderCount: 7, Months: []artifact.MonthCount{
				{Month: "2026-11", Count: 3}, {Month: "2026-09", Count: 12},
			}},
			Summarized: &summarized,
		},
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary?since=2026-10-26T00%3A00%3A00Z&type=images", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if summarized.Type != artifact.ListTypeImages || !summarized.Since.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("summary options = %+v", summarized)
	}
	// No tz cuts the months in UTC.
	if summarized.Location != time.UTC {
		t.Fatalf("summary location = %v, want UTC", summarized.Location)
	}
	if got, want := rec.Body.String(), `{"olderCount":7,"months":[{"month":"2026-11","count":3},{"month":"2026-09","count":12}]}`+"\n"; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestArtifactSummaryCutsMonthsInTheClientZone(t *testing.T) {
	var summarized artifact.SummaryOptions
	server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{Summarized: &summarized}})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary?tz=Pacific%2FAuckland", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if summarized.Location == nil || summarized.Location.String() != "Pacific/Auckland" {
		t.Fatalf("summary location = %v, want Pacific/Auckland", summarized.Location)
	}
}

func TestArtifactSummaryEmptyMonthsIsAnArray(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{}})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), `{"olderCount":0,"months":[]}`+"\n"; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestArtifactSummaryRejectsBadParams(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{}})
	for query, want := range map[string]string{
		"since=nope":  "invalid since",
		"type=videos": "invalid type",
	} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary?"+query, ""))
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("%s: status = %d body = %s, want 400 %q", query, rec.Code, rec.Body.String(), want)
		}
	}
}

// The client always sends the browser's zone; one the server cannot load (a
// bogus name, one newer than its tzdata, the server's own Local) cuts the
// months in UTC rather than leaving the period picker without a summary.
func TestArtifactSummaryUnknownZoneFallsBackToUTC(t *testing.T) {
	for _, tz := range []string{"Mars%2FOlympus", "Local"} {
		var summarized artifact.SummaryOptions
		server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{Summarized: &summarized}})
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary?tz="+tz, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("tz=%s: status = %d body = %s, want 200", tz, rec.Code, rec.Body.String())
		}
		if summarized.Location != time.UTC {
			t.Fatalf("tz=%s: summary location = %v, want UTC", tz, summarized.Location)
		}
	}
}

func TestArtifactSummaryWithoutArtifactStoreIsNotFound(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/summary?type=files", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListArtifactsOptionsKeepSortAndTypeWithPeriod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/artifacts?type=files&sort=name&order=asc&limit=5&cursor=c&until=2026-11-01T00%3A00%3A00Z", nil)
	opts, err := listArtifactsOptionsFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Type != artifact.ListTypeFiles || opts.Sort != artifact.SortByName || opts.Order != artifact.SortAsc ||
		opts.Limit != 5 || opts.Cursor != "c" || !opts.Since.IsZero() || !opts.Until.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("options = %+v", opts)
	}
}

func TestArtifactSummaryRequiresAuth(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{Artifacts: fakeArtifactStore{}})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/artifacts/summary", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
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
		Artifacts: fakeArtifactStore{Artifacts: items},
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

func TestImageUploadErrorStatuses(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		err  error
		want int
	}{
		{artifact.ErrThreadUploadLimit, http.StatusConflict},
		{artifact.ErrUploadTooLarge, http.StatusRequestEntityTooLarge},
		{&artifact.UploadError{Stage: artifact.UploadStageCount, Err: boom}, http.StatusInternalServerError},
		{&artifact.UploadError{Stage: artifact.UploadStageFile, Err: boom}, http.StatusBadRequest},
		{&artifact.UploadError{Stage: artifact.UploadStageWrite, Err: boom}, http.StatusInternalServerError},
		{&artifact.UploadError{Stage: artifact.UploadStageRecord, Err: boom}, http.StatusInternalServerError},
		{boom, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeImageUploadError(rec, httptest.NewRequest(http.MethodPost, "/api/artifacts/images/upload", nil), tc.err)
		if rec.Code != tc.want {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

// An image declaring more pixels than imagescale will decode is refused at
// upload, so it never reaches a model at full size.
func TestUploadImageAttachmentRejectsTooManyPixels(t *testing.T) {
	server := newAuthenticatedServer(t, Deps{
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], 20000)
	binary.BigEndian.PutUint32(ihdr[4:8], 20000)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit truecolor
	huge := []byte("\x89PNG\r\n\x1a\n")
	huge = binary.BigEndian.AppendUint32(huge, uint32(len(ihdr)))
	huge = append(huge, "IHDR"...)
	huge = append(huge, ihdr...)
	huge = binary.BigEndian.AppendUint32(huge, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))

	body, contentType := multipartUploadBody(t, "file", "huge.png", "image/png", huge)
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
		Deleted: &deleted,
		Artifacts: []artifact.Artifact{
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

// An upload that backs a knowledge document must go through the document
// delete: removing only the artifact left the document embedded, so its chunks
// kept feeding answers after the user deleted the file.
func TestDeleteArtifactDeletesTheDocumentItBacks(t *testing.T) {
	deleted := []string{}
	store := fakeArtifactStore{
		Deleted: &deleted,
		Artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "user_1", VolumeRelPath: "files/uploads/a.pdf", DisplayFilename: "a.pdf", MIMEType: "application/pdf"},
		},
	}
	docs := &fakeDocumentService{BackingArtifactID: "art_1"}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, Documents: docs, UsersDir: t.TempDir()})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/artifacts/art_1", ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(docs.DeletedForArtifact) != 1 || docs.DeletedForArtifact[0] != "art_1" {
		t.Fatalf("document deletes = %v, want the one backed by art_1", docs.DeletedForArtifact)
	}
}

func TestDeleteArtifactRejectsAnotherUsersArtifact(t *testing.T) {
	deleted := []string{}
	store := fakeArtifactStore{
		Deleted: &deleted,
		Artifacts: []artifact.Artifact{
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
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	if err := os.WriteFile(absFile, pngFileBytes(t, 800, 600), 0o644); err != nil {
		t.Fatal(err)
	}
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 1234},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail?v=2", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type = %q, want image/jpeg", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != artifactCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cc, artifactCacheControl)
	}
	// Not the older, smaller thumbnail's validator: a revalidation of that one
	// must not come back 304.
	if etag := rec.Header().Get("ETag"); etag != `"thumb-v2-art_1-1234"` {
		t.Fatalf("ETag = %s, want the tile-sized thumbnail's", etag)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode thumbnail body: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("thumbnail format = %q, want jpeg", format)
	}
	// The shorter side fills a square tile.
	if cfg.Width != 427 || cfg.Height != artifact.ThumbnailShortSide {
		t.Fatalf("thumbnail = %dx%d, want 427x%d", cfg.Width, cfg.Height, artifact.ThumbnailShortSide)
	}
	// The sidecar file was written into the reserved thumbnail subtree...
	wantThumbRel := artifact.ThumbnailRelPath(relPath)
	thumbAbs := filepath.Join(usersDir, "user_1", filepath.FromSlash(wantThumbRel))
	if _, err := os.Stat(thumbAbs); err != nil {
		t.Fatalf("thumbnail sidecar not written: %v", err)
	}
	// ...and the relpath was backfilled onto the row for future requests.
	if got := store.Artifacts[0].ThumbnailRelPath; got != wantThumbRel {
		t.Fatalf("ThumbnailRelPath = %q, want %q (lazy backfill persisted)", got, wantThumbRel)
	}
}

// writeUserBytes writes data at a volume-relative path under the user's root.
func writeUserBytes(t *testing.T, usersDir, relPath string, data []byte) string {
	t.Helper()
	abs := filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

// A thumbnail from before the tile size (the row's stored path is the older
// scheme's) is regenerated at the tile size on its next view, recorded, and
// the small one removed.
func TestThumbnailArtifactRegeneratesAnOlderSmallThumbnail(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	writeUserBytes(t, usersDir, relPath, pngFileBytes(t, 800, 600))
	legacyRel := ".loom/thumbnails/" + relPath + ".jpg"
	legacyAbs := writeUserBytes(t, usersDir, legacyRel, []byte("small-jpeg"))
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 1234, ThumbnailRelPath: legacyRel},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode thumbnail body (the small one served?): %v", err)
	}
	if cfg.Height != artifact.ThumbnailShortSide {
		t.Fatalf("thumbnail height = %d, want %d", cfg.Height, artifact.ThumbnailShortSide)
	}
	if got, want := store.Artifacts[0].ThumbnailRelPath, artifact.ThumbnailRelPath(relPath); got != want {
		t.Fatalf("ThumbnailRelPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(legacyAbs); !os.IsNotExist(err) {
		t.Fatalf("small thumbnail still on disk, err = %v", err)
	}

	// The next view serves the recorded one as is, even with the original gone.
	if err := os.Remove(filepath.Join(usersDir, "user_1", filepath.FromSlash(relPath))); err != nil {
		t.Fatal(err)
	}
	again := httptest.NewRecorder()
	server.ServeHTTP(again, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail?v=2", ""))
	if again.Code != http.StatusOK || !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) {
		t.Fatalf("second view: status = %d, want 200 with the same thumbnail", again.Code)
	}
}

// When the tile size cannot be made (the original is gone), the older small
// thumbnail still beats a 404.
func TestThumbnailArtifactFallsBackToTheOlderThumbnail(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	legacyRel := ".loom/thumbnails/" + relPath + ".jpg"
	writeUserBytes(t, usersDir, legacyRel, []byte("small-jpeg"))
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
		{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 1234, ThumbnailRelPath: legacyRel},
	}}
	server := newAuthenticatedServer(t, Deps{Artifacts: store, UsersDir: usersDir})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail", ""))
	if rec.Code != http.StatusOK || rec.Body.String() != "small-jpeg" {
		t.Fatalf("status = %d body = %q, want the small thumbnail", rec.Code, rec.Body.String())
	}
	// A stand-in on the current size's URL: never cached and without the
	// current size's validator, so the next view asks again.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if etag := rec.Header().Get("ETag"); etag != "" {
		t.Fatalf("ETag = %q, want none", etag)
	}
	if lm := rec.Header().Get("Last-Modified"); lm != "" {
		t.Fatalf("Last-Modified = %q, want none", lm)
	}
	if got := store.Artifacts[0].ThumbnailRelPath; got != legacyRel {
		t.Fatalf("ThumbnailRelPath = %q, want it left at %q", got, legacyRel)
	}

	// Neither size, no original: a 404.
	_ = os.Remove(filepath.Join(usersDir, "user_1", filepath.FromSlash(legacyRel)))
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/artifacts/art_1/thumbnail", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// thumbnailTestServer is a server over one image artifact whose original is on
// disk, its thumbnail generator replaced by gen.
func thumbnailTestServer(t *testing.T, gen func(string, artifact.Artifact) (string, error)) (*server, artifact.Artifact, string) {
	t.Helper()
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relPath := "files/outputs/art_1.png"
	writeUserBytes(t, usersDir, relPath, pngFileBytes(t, 40, 30))
	found := artifact.Artifact{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", SizeBytes: 1234}
	s := newServer(Deps{Artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{found}}, UsersDir: usersDir})
	s.thumbnailGenerator = gen
	return s, found, usersDir
}

// With every regeneration slot taken, a request waits; one whose client went
// away leaves the queue without decoding anything.
func TestThumbnailRegenerationWaitsForASlot(t *testing.T) {
	var calls atomic.Int32
	s, found, _ := thumbnailTestServer(t, func(string, artifact.Artifact) (string, error) {
		calls.Add(1)
		return "", errors.New("not decoded")
	})
	for range cap(s.thumbnailSlots) {
		s.thumbnailSlots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.resolveOrCreateThumbnail(ctx, "user_1", found); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("generator ran %d times while every slot was taken", n)
	}

	// A freed slot lets the next one through.
	<-s.thumbnailSlots
	if _, _, err := s.resolveOrCreateThumbnail(context.Background(), "user_1", found); err == nil {
		t.Fatal("err = nil, want the generator's error")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("generator ran %d times, want 1", n)
	}
	if n := len(s.thumbnailSlots); n != cap(s.thumbnailSlots)-1 {
		t.Fatalf("slots taken = %d after the request, want %d (its slot released)", n, cap(s.thumbnailSlots)-1)
	}
}

// Requests on screen at once never decode more than the slots allow.
func TestThumbnailRegenerationIsBounded(t *testing.T) {
	var inFlight, peak atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	s, found, _ := thumbnailTestServer(t, func(string, artifact.Artifact) (string, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		inFlight.Add(-1)
		return "", errors.New("not decoded")
	})
	const requests = 6
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			_, _, _ = s.resolveOrCreateThumbnail(context.Background(), "user_1", found)
		})
	}
	for range cap(s.thumbnailSlots) {
		<-entered
	}
	close(release)
	wg.Wait()
	if p := peak.Load(); p > int32(cap(s.thumbnailSlots)) {
		t.Fatalf("%d decodes at once, want at most %d", p, cap(s.thumbnailSlots))
	}
	if n := len(entered) + cap(s.thumbnailSlots); n != requests {
		t.Fatalf("generator ran %d times, want %d", n, requests)
	}
}

// A request that waited for its slot finds the current size another one wrote
// meanwhile and serves it instead of decoding the original again.
func TestThumbnailRegenerationRechecksAfterTheWait(t *testing.T) {
	var calls atomic.Int32
	s, found, usersDir := thumbnailTestServer(t, func(string, artifact.Artifact) (string, error) {
		calls.Add(1)
		return "", errors.New("not decoded")
	})
	thumbRel := artifact.ThumbnailRelPath(found.VolumeRelPath)
	writeUserBytes(t, usersDir, thumbRel, []byte("fresh-jpeg"))
	legacyAbs := writeUserBytes(t, usersDir, ".loom/thumbnails/"+found.VolumeRelPath+".jpg", []byte("small-jpeg"))

	abs, legacy, err := s.resolveOrCreateThumbnail(context.Background(), "user_1", found)
	if err != nil || legacy {
		t.Fatalf("err = %v legacy = %v, want the current size", err, legacy)
	}
	if got, _ := os.ReadFile(abs); string(got) != "fresh-jpeg" { //nolint:gosec // test temp dir
		t.Fatalf("served %q, want the one written meanwhile", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("generator ran %d times, want 0", n)
	}
	// The row converges on it and the small one goes.
	stored, _, _ := s.artifacts.Get(context.Background(), "user_1", found.ID)
	if stored.ThumbnailRelPath != thumbRel {
		t.Fatalf("ThumbnailRelPath = %q, want %q", stored.ThumbnailRelPath, thumbRel)
	}
	if _, err := os.Stat(legacyAbs); !os.IsNotExist(err) {
		t.Fatalf("small thumbnail still on disk, err = %v", err)
	}
}

func TestThumbnailArtifactNonImageReturns404(t *testing.T) {
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
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
	thumbRel := artifact.ThumbnailRelPath(relPath)
	thumbFile := filepath.Join(usersDir, "user_1", filepath.FromSlash(thumbRel))
	if err := os.MkdirAll(filepath.Dir(thumbFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbFile, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An older, smaller thumbnail never regenerated goes too.
	legacyFile := writeUserBytes(t, usersDir, ".loom/thumbnails/"+relPath+".jpg", []byte("jpg"))
	deleted := []string{}
	store := fakeArtifactStore{
		Deleted: &deleted,
		Artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: "user_1", VolumeRelPath: relPath, DisplayFilename: "a.png", MIMEType: "image/png", ThumbnailRelPath: thumbRel},
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
	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Fatalf("older thumbnail sidecar still exists, err = %v", err)
	}
}
