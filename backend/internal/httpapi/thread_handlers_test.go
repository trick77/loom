package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/chat"
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
