package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/spark/internal/chat"
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
