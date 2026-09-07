package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

// writeUserFile creates a file at a volume-relative path under the user's root
// and returns its absolute path.
func writeUserFile(t *testing.T, usersDir, relPath string) string {
	t.Helper()
	abs := filepath.Join(usersDir, testUser.ID, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	return abs
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

// A raster artifact's sidecar thumbnail lives outside its own relpath, so it has
// to be removed explicitly or every image in a deleted thread leaves a JPEG in
// the reserved .loom/thumbnails subtree.
func TestDeleteThreadRemovesArtifactThumbnails(t *testing.T) {
	usersDir := t.TempDir()
	relPath := "files/outputs/chart.png"
	absArtifact := writeUserFile(t, usersDir, relPath)
	absThumbnail := writeUserFile(t, usersDir, artifact.ThumbnailRelPath(relPath))

	srv := newAuthenticatedServer(t, Deps{
		Thread: &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}},
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:               "art_1",
			UserID:           testUser.ID,
			ThreadID:         "thr_1",
			VolumeRelPath:    relPath,
			ThumbnailRelPath: artifact.ThumbnailRelPath(relPath),
		}}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if exists(t, absArtifact) {
		t.Error("artifact file still exists")
	}
	if exists(t, absThumbnail) {
		t.Error("sidecar thumbnail still exists")
	}
}

// A project knowledge document uploaded from inside a project thread has an
// artifact row carrying that thread id as provenance only. Deleting the thread
// must detach it rather than let the cascade reach it (which aborts the delete on
// documents.user_id, see documents.TestDeleteThreadKeepsProjectDocument...) and
// must leave its bytes, which the surviving document still needs.
func TestDeleteThreadSparesArtifactsBackingSurvivingDocuments(t *testing.T) {
	usersDir := t.TempDir()
	keptRel, sweptRel := "projects/prj_1/shared.txt", "files/outputs/chart.png"
	absKept := writeUserFile(t, usersDir, keptRel)
	absSwept := writeUserFile(t, usersDir, sweptRel)
	var detached []string

	docs := &fakeDocumentService{artifactsInUse: []string{"art_kept"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}},
		Documents: docs,
		Artifacts: fakeArtifactStore{
			detached: &detached,
			artifacts: []artifact.Artifact{
				{ID: "art_kept", UserID: testUser.ID, ThreadID: "thr_1", VolumeRelPath: keptRel},
				{ID: "art_swept", UserID: testUser.ID, ThreadID: "thr_1", VolumeRelPath: sweptRel},
			},
		},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(detached) != 1 || detached[0] != "art_kept" {
		t.Fatalf("DetachFromThread ids = %v, want [art_kept]", detached)
	}
	if !exists(t, absKept) {
		t.Error("file of an artifact backing a surviving document was deleted")
	}
	if exists(t, absSwept) {
		t.Error("thread-owned artifact file still exists")
	}
	// The in-use query only tells survivors apart once the thread's own documents
	// are gone, so it must run after DeleteThreadData.
	if len(docs.deletedThreadData) != 1 || len(docs.inUseQueriedThreads) != 1 {
		t.Fatalf("DeleteThreadData=%v, in-use queries=%v, want one each", docs.deletedThreadData, docs.inUseQueriedThreads)
	}
}

// A failure anywhere in the cleanup must fail the delete rather than half-apply
// it, and must not leave the artifact files removed behind a 500.
func TestDeleteThreadFailsWhenArtifactRetentionFails(t *testing.T) {
	usersDir := t.TempDir()
	relPath := "files/outputs/chart.png"
	abs := writeUserFile(t, usersDir, relPath)

	store := &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		Documents: &fakeDocumentService{artifactsInUseErr: errors.New("boom")},
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: testUser.ID, ThreadID: "thr_1", VolumeRelPath: relPath},
		}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", ""))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if len(store.deletedThreads) != 0 {
		t.Errorf("thread deleted despite cleanup failure: %v", store.deletedThreads)
	}
	if !exists(t, abs) {
		t.Error("artifact file removed despite cleanup failure")
	}
}

// The bulk loop skips a thread it cannot clean up rather than aborting the batch;
// the skip has to be visible, not silent.
func TestBulkDeleteThreadsSkipsThreadWhenRAGCleanupFails(t *testing.T) {
	store := &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		Documents: &fakeDocumentService{deleteDataErr: errors.New("boom")},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads:delete", `{"threadIds":["thr_1"]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "{\"deleted\":0}\n" && body != "{\"deleted\":0}" {
		t.Fatalf("body = %q, want deleted 0", body)
	}
	if len(store.deletedThreads) != 0 {
		t.Errorf("thread deleted despite failed knowledge cleanup: %v", store.deletedThreads)
	}
}
