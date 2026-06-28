package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

// TestPublicShare_unknownReturns404 verifies the public endpoint needs no auth and
// does not reveal whether a share exists, and always sets the noindex header.
func TestPublicShare_unknownReturns404(t *testing.T) {
	// No Auth in Deps at all — proves the route bypasses authentication.
	srv := New(Deps{Thread: &fakeThreadStore{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/shares/does-not-exist", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Fatalf("X-Robots-Tag = %q, want noindex", got)
	}
}

// TestShare_createViewDisableFlow exercises the whole lifecycle at the HTTP layer:
// an owner creates a share, an ANONYMOUS request reads the sanitized snapshot, and
// once disabled the public link 404s.
func TestShare_createViewDisableFlow(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "t1", UserID: testUser.ID, Title: "My Thread"},
		messages: []chat.Message{
			{ID: "m1", ThreadID: "t1", Role: chat.RoleUser, Content: "hello there"},
			{
				ID:               "m2",
				ThreadID:         "t1",
				Role:             chat.RoleAssistant,
				Content:          "the answer",
				ReasoningContent: "SECRET_REASONING",
				Citations:        json.RawMessage(`[{"filename":"SECRET_DOC.pdf"}]`),
			},
		},
	}
	authed := newAuthenticatedServer(t, Deps{Thread: store, PublicURL: "https://loom.example.com"})
	anon := New(Deps{Thread: store}) // no Auth: the anonymous viewer

	// Owner creates the share.
	rec := httptest.NewRecorder()
	authed.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/t1/share", ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created shareSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ShareID == "" || !created.Shared {
		t.Fatalf("unexpected summary: %+v", created)
	}
	if created.ShareURL != "https://loom.example.com/share/"+created.ShareID {
		t.Fatalf("shareUrl = %q", created.ShareURL)
	}

	// Anonymous viewer reads the snapshot.
	rec = httptest.NewRecorder()
	anon.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shares/"+created.ShareID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public get status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"hello there", "the answer", "My Thread", "jan"} {
		if !strings.Contains(body, want) {
			t.Errorf("public body missing %q\n%s", want, body)
		}
	}
	for _, bad := range []string{"SECRET_REASONING", "SECRET_DOC.pdf"} {
		if strings.Contains(body, bad) {
			t.Errorf("public body LEAKED %q\n%s", bad, body)
		}
	}

	// Owner disables the link.
	rec = httptest.NewRecorder()
	authed.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/t1/share", ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// Public link now 404s.
	rec = httptest.NewRecorder()
	anon.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shares/"+created.ShareID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled get status = %d, want 404", rec.Code)
	}
}
