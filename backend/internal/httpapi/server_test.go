package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/spark/internal/auth"
)

func TestHealth_returnsOK(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
	if body["version"] != "test" {
		t.Errorf("version field = %q, want test", body["version"])
	}
}

func TestRecovery_turnsPanicInto500(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})
	h := recovery(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestMeRequiresSession(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMeReturnsCurrentUser(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", DisplayName: "Jan", Role: auth.RoleAdmin, ResponseLanguage: "auto"}
	srv := New(Deps{
		Version:  "test",
		Auth:     auth.NewMiddleware(fakeSessionStore{session: auth.Session{Token: "tok", UserID: user.ID}, ok: true}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{session: auth.Session{Token: "tok", UserID: user.ID}, ok: true},
		Users:    fakeUserStore{user: user, ok: true},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body auth.User
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Role != auth.RoleAdmin {
		t.Fatalf("role = %q, want admin", body.Role)
	}
}

func TestAdminUsersRequiresAdmin(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "auto"}
	srv := New(Deps{
		Version:  "test",
		Auth:     auth.NewMiddleware(fakeSessionStore{session: auth.Session{Token: "tok", UserID: user.ID}, ok: true}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{session: auth.Session{Token: "tok", UserID: user.ID}, ok: true},
		Users:    fakeUserStore{user: user, ok: true},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

type fakeSessionStore struct {
	session auth.Session
	ok      bool
}

func (f fakeSessionStore) Lookup(context.Context, string) (auth.Session, bool, error) {
	return f.session, f.ok, nil
}

func (f fakeSessionStore) Create(context.Context, string, time.Duration) (auth.Session, error) {
	return f.session, nil
}

func (f fakeSessionStore) Revoke(context.Context, string) error {
	return nil
}

func (f fakeSessionStore) CookieFor(token string, expires time.Time) *http.Cookie {
	return (&auth.SessionStore{}).CookieFor(token, expires)
}

func (f fakeSessionStore) ClearCookie() *http.Cookie {
	return (&auth.SessionStore{}).ClearCookie()
}

type fakeUserStore struct {
	user auth.User
	ok   bool
}

func (f fakeUserStore) FindByID(context.Context, string) (auth.User, bool, error) {
	return f.user, f.ok, nil
}

func (f fakeUserStore) UpsertFromClaims(context.Context, auth.Claims, string) (auth.User, error) {
	return f.user, nil
}

func (f fakeUserStore) ListUsers(context.Context) ([]auth.User, error) {
	return []auth.User{f.user}, nil
}
