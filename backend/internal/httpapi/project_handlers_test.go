package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

func TestCreateProjectReturns201(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects", `{"name":"School","description":"Homework"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var body chat.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Name != "School" {
		t.Fatalf("name = %q, want School", body.Name)
	}
}

func TestCreateProjectRejectsOversizedRequestBody(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects", strings.Repeat(" ", maxJSONBodyBytes+1))

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func TestArchiveAndDeleteProjectReturn204(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "archive", method: http.MethodPost, path: "/api/projects/proj_1/archive"},
		{name: "delete", method: http.MethodDelete, path: "/api/projects/proj_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeThreadStore{
				Project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"},
			}
			srv := newAuthenticatedServer(t, Deps{Thread: store})
			rec := httptest.NewRecorder()
			req := authenticatedRequest(tc.method, tc.path, "")

			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestStarAndUnstarProjectReturnUpdatedProject(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "star", path: "/api/projects/proj_1/star", expected: true},
		{name: "unstar", path: "/api/projects/proj_1/unstar", expected: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeThreadStore{
				Project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"},
			}
			srv := newAuthenticatedServer(t, Deps{Thread: store})
			rec := httptest.NewRecorder()
			req := authenticatedRequest(http.MethodPost, tc.path, "")

			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			var got chat.Project
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Starred != tc.expected {
				t.Fatalf("project Starred = %v, want %v", got.Starred, tc.expected)
			}
		})
	}
}

func TestStarProjectNotFoundReturns404(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/missing/star", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteProjectRemovesGeneratedArtifactFiles(t *testing.T) {
	usersDir := t.TempDir()
	projectID := "proj_1"
	relPath := "projects/proj_1/outputs/report.txt"
	absPath := filepath.Join(usersDir, testUser.ID, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeThreadStore{
		Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "School"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		Artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{{
			ID:            "art_1",
			UserID:        testUser.ID,
			ThreadID:      "thr_1",
			ProjectID:     &projectID,
			VolumeRelPath: relPath,
		}}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodDelete, "/api/projects/proj_1", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Fatalf("artifact file still exists or stat failed: %v", err)
	}
}

// The project's threads cascade with it, and their artifacts with them. One
// backing a document that outlives the project (another project's knowledge,
// uploaded from one of these threads) must be detached first, as a thread
// delete does: the cascade reaching it aborts the delete on documents.user_id,
// and its bytes are still needed.
func TestDeleteProjectSparesArtifactsBackingSurvivingDocuments(t *testing.T) {
	usersDir := t.TempDir()
	projectID := "proj_1"
	keptRel, sweptRel := "projects/prj_2/shared.txt", "projects/proj_1/outputs/chart.png"
	absKept := writeUserFile(t, usersDir, keptRel)
	absSwept := writeUserFile(t, usersDir, sweptRel)
	var detached []string

	docs := &fakeDocumentService{ArtifactsInUse: []string{"art_kept"}}
	srv := newAuthenticatedServer(t, Deps{
		Thread: &fakeThreadStore{
			Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "School"},
			Thread:  chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ProjectID: &projectID},
		},
		Documents: docs,
		Artifacts: fakeArtifactStore{
			Detached: &detached,
			Artifacts: []artifact.Artifact{
				{ID: "art_kept", UserID: testUser.ID, ThreadID: "thr_1", ProjectID: &projectID, VolumeRelPath: keptRel},
				{ID: "art_swept", UserID: testUser.ID, ThreadID: "thr_1", ProjectID: &projectID, VolumeRelPath: sweptRel},
			},
		},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/projects/proj_1", ""))

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
		t.Error("project thread's artifact file still exists")
	}
}

// A failed in-use check fails the delete before anything is removed.
func TestDeleteProjectFailsWhenArtifactRetentionFails(t *testing.T) {
	usersDir := t.TempDir()
	projectID := "proj_1"
	relPath := "projects/proj_1/outputs/chart.png"
	abs := writeUserFile(t, usersDir, relPath)
	store := &fakeThreadStore{
		Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "School"},
		Thread:  chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ProjectID: &projectID},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread:    store,
		Documents: &fakeDocumentService{ArtifactsInUseErr: errors.New("boom")},
		Artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: testUser.ID, ThreadID: "thr_1", ProjectID: &projectID, VolumeRelPath: relPath},
		}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/projects/proj_1", ""))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !exists(t, abs) {
		t.Error("artifact file removed despite the failed delete")
	}
}

func TestDeleteProjectPurgesProjectRAGData(t *testing.T) {
	store := &fakeThreadStore{Project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"}}
	docs := &fakeDocumentService{}
	srv := newAuthenticatedServer(t, Deps{Thread: store, Documents: docs})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodDelete, "/api/projects/proj_1", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(docs.DeletedProjectData) != 1 || docs.DeletedProjectData[0] != "proj_1" {
		t.Fatalf("DeleteProjectData calls = %v, want [proj_1]", docs.DeletedProjectData)
	}
}

func TestArchiveProjectNotFoundReturns404(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/missing/archive", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
