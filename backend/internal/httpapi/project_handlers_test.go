package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/lume/internal/artifact"
	"github.com/trick77/lume/internal/chat"
)

func TestCreateProjectReturns201(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
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
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects", strings.Repeat(" ", maxJSONBodyBytes+1))

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
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
			store := &fakeChatStore{
				project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"},
			}
			srv := newAuthenticatedChatServer(t, Deps{Chat: store})
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
			store := &fakeChatStore{
				project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"},
			}
			srv := newAuthenticatedChatServer(t, Deps{Chat: store})
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
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
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
	store := &fakeChatStore{
		project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "School"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
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

func TestDeleteProjectPurgesProjectRAGData(t *testing.T) {
	store := &fakeChatStore{project: chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "School"}}
	docs := &fakeDocumentService{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store, Documents: docs})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodDelete, "/api/projects/proj_1", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(docs.deletedProjectData) != 1 || docs.deletedProjectData[0] != "proj_1" {
		t.Fatalf("DeleteProjectData calls = %v, want [proj_1]", docs.deletedProjectData)
	}
}

func TestArchiveProjectNotFoundReturns404(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/missing/archive", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
