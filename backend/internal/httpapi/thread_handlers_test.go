package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/store"
)

func TestCreateThreadRequiresAuth(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{}`))

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateThreadReturnsThread(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads", `{}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var body chat.Thread
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Title != chat.DefaultThreadTitle {
		t.Fatalf("title = %q, want %q", body.Title, chat.DefaultThreadTitle)
	}
}

func TestListThreadsUsesCurrentUserScope(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.listThreadsUserID != testUser.ID {
		t.Fatalf("ListThreads userID = %q, want %q", store.listThreadsUserID, testUser.ID)
	}
}

func TestListThreadsParsesQueryOptions(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads?projectId=null&starred=true&archived=true&limit=12", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !store.listThreadsOptions.ProjectlessOnly {
		t.Fatal("ProjectlessOnly = false, want true")
	}
	if !store.listThreadsOptions.StarredOnly {
		t.Fatal("StarredOnly = false, want true")
	}
	if !store.listThreadsOptions.Archived {
		t.Fatal("Archived = false, want true")
	}
	if store.listThreadsOptions.Limit != 12 {
		t.Fatalf("Limit = %d, want 12", store.listThreadsOptions.Limit)
	}
}

func TestGetThreadNotFoundReturns404(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads/missing", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestGetThreadReturns404ForAnotherUsersThread(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, userID := range []string{"alice", "bob"} {
		_, err := db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, role)
VALUES (?, ?, ?, 'user')`,
			userID, "subject-"+userID, userID,
		)
		if err != nil {
			t.Fatalf("insert user %s: %v", userID, err)
		}
	}
	chatStore := chat.NewStore(db)
	bobThread, err := chatStore.CreateThread(ctx, "bob", chat.CreateThreadInput{Title: "Bob private thread"})
	if err != nil {
		t.Fatalf("create bob thread: %v", err)
	}
	alice := auth.User{ID: "alice", Username: "alice", Role: auth.RoleUser, ResponseLanguage: "auto"}
	srv := newAuthenticatedChatServerForUser(t, alice, Deps{Chat: chatStore})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads/"+bobThread.ID, "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestArchiveAndDeleteThreadReturn204(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "archive", method: http.MethodPost, path: "/api/threads/thr_1/archive"},
		{name: "delete", method: http.MethodDelete, path: "/api/threads/thr_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeChatStore{
				thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"},
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

func TestUpdateThreadCanMoveIntoProject(t *testing.T) {
	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPatch, "/api/threads/thr_1", `{"projectId":"proj_1"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !store.updateThreadInput.ProjectID.Set {
		t.Fatal("ProjectID.Set = false, want true")
	}
	if store.updateThreadInput.ProjectID.Value == nil || *store.updateThreadInput.ProjectID.Value != "proj_1" {
		t.Fatalf("ProjectID.Value = %v, want proj_1", store.updateThreadInput.ProjectID.Value)
	}
}

func TestUpdateThreadCanRemoveFromProject(t *testing.T) {
	projectID := "proj_1"
	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, ProjectID: &projectID, Title: "Thread"}}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPatch, "/api/threads/thr_1", `{"projectId":null}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !store.updateThreadInput.ProjectID.Set {
		t.Fatal("ProjectID.Set = false, want true")
	}
	if store.updateThreadInput.ProjectID.Value != nil {
		t.Fatalf("ProjectID.Value = %v, want nil", store.updateThreadInput.ProjectID.Value)
	}
}

func TestUpdateThreadProjectNotFoundReturns404(t *testing.T) {
	store := &fakeChatStore{
		thread:          chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"},
		updateThreadErr: errors.New("project not found"),
	}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPatch, "/api/threads/thr_1", `{"projectId":"missing"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteThreadRemovesGeneratedArtifactFiles(t *testing.T) {
	usersDir := t.TempDir()
	relPath := "files/outputs/report.txt"
	absPath := filepath.Join(usersDir, testUser.ID, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:            "art_1",
			UserID:        testUser.ID,
			ThreadID:      "thr_1",
			VolumeRelPath: relPath,
		}}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Fatalf("artifact file still exists or stat failed: %v", err)
	}
}

func TestDeleteThreadPurgesThreadRAGData(t *testing.T) {
	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	docs := &fakeDocumentService{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store, Documents: docs})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(docs.deletedThreadData) != 1 || docs.deletedThreadData[0] != "thr_1" {
		t.Fatalf("DeleteThreadData calls = %v, want [thr_1]", docs.deletedThreadData)
	}
}

func TestBulkDeleteThreadsPurgesThreadRAGData(t *testing.T) {
	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	docs := &fakeDocumentService{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store, Documents: docs})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads:delete", `{"threadIds":["thr_1","thr_2"]}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(docs.deletedThreadData) != 2 {
		t.Fatalf("DeleteThreadData calls = %v, want 2", docs.deletedThreadData)
	}
}

func TestBulkDeleteThreadsRemovesArtifactsAndCountsDeleted(t *testing.T) {
	usersDir := t.TempDir()
	writeArtifact := func(rel string) string {
		abs := filepath.Join(usersDir, testUser.ID, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		return abs
	}
	absOne := writeArtifact("files/outputs/one.txt")
	absTwo := writeArtifact("files/outputs/two.txt")

	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		Artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{
			{ID: "art_1", UserID: testUser.ID, ThreadID: "thr_1", VolumeRelPath: "files/outputs/one.txt"},
			{ID: "art_2", UserID: testUser.ID, ThreadID: "thr_2", VolumeRelPath: "files/outputs/two.txt"},
		}},
		UsersDir: usersDir,
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads:delete", `{"threadIds":["thr_1","thr_2"]}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", resp.Deleted)
	}
	if _, err := os.Stat(absOne); !os.IsNotExist(err) {
		t.Fatalf("artifact one still exists: %v", err)
	}
	if _, err := os.Stat(absTwo); !os.IsNotExist(err) {
		t.Fatalf("artifact two still exists: %v", err)
	}
}

func TestBulkDeleteThreadsSkipsEmptyAndDuplicateIDs(t *testing.T) {
	store := &fakeChatStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads:delete", `{"threadIds":["thr_1","thr_1",""]}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (dedup + skip empty)", resp.Deleted)
	}
}

func TestCreateThreadRejectsUnknownJSONFields(t *testing.T) {
	store := &fakeChatStore{}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads", `{"unexpected":true}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateThreadHidesInternalStoreErrors(t *testing.T) {
	store := &fakeChatStore{createThreadErr: errors.New("insert thread: database unavailable")}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads", `{}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "database unavailable") {
		t.Fatalf("body leaks internal error: %s", rec.Body.String())
	}
}

func TestListThreadsReturns503WhenChatDependencyMissing(t *testing.T) {
	srv := newAuthenticatedChatServer(t, Deps{})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"chat is not configured"`) {
		t.Fatalf("body = %q, want chat configuration error", rec.Body.String())
	}
}
