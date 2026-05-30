package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuthRejectsMissingSession(t *testing.T) {
	mw := NewMiddleware(fakeSessionLookup{}, fakeUserLookup{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	mw.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAuthStoresUserInContext(t *testing.T) {
	user := User{ID: "u1", Username: "jan", Role: RoleUser}
	mw := NewMiddleware(
		fakeSessionLookup{session: Session{Token: "tok", UserID: "u1"}, ok: true},
		fakeUserLookup{user: user, ok: true},
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "tok"})

	mw.RequireAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := UserFromContext(r.Context())
		if !ok {
			t.Fatal("user missing from context")
		}
		if got.ID != user.ID {
			t.Fatalf("user id = %q, want %q", got.ID, user.ID)
		}
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequireAdminRejectsRegularUser(t *testing.T) {
	mw := NewMiddleware(fakeSessionLookup{}, fakeUserLookup{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, User{ID: "u1", Role: RoleUser}))

	mw.RequireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

type fakeSessionLookup struct {
	session Session
	ok      bool
	err     error
}

func (f fakeSessionLookup) Lookup(context.Context, string) (Session, bool, error) {
	return f.session, f.ok, f.err
}

type fakeUserLookup struct {
	user User
	ok   bool
	err  error
}

func (f fakeUserLookup) FindByID(context.Context, string) (User, bool, error) {
	return f.user, f.ok, f.err
}
