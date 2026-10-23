package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kitauth "github.com/trick77/ship/kit/auth"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// sessionCookie is the cookie a login for userID sets.
func sessionCookie(t *testing.T, sessions *kitauth.Sessions, userID string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := sessions.Issue(rec, SessionFor(userID)); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return rec.Result().Cookies()[0]
}

func testSessions(t *testing.T) *kitauth.Sessions {
	t.Helper()
	s, err := NewSessions(testSecret, time.Hour, false)
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}
	return s
}

func TestNewSessionsRejectsShortSecret(t *testing.T) {
	if _, err := NewSessions("too-short", time.Hour, false); err == nil || !strings.Contains(err.Error(), "BACKEND_SESSION_SECRET") {
		t.Fatalf("err = %v, want a BACKEND_SESSION_SECRET length error", err)
	}
}

func TestRequireAuthRejectsMissingSession(t *testing.T) {
	mw := NewMiddleware(testSessions(t), fakeUserLookup{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	mw.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A cookie from before the move to signed sessions (an opaque token) or one
// signed with another secret is no session.
func TestRequireAuthRejectsUnsignedOrForeignCookie(t *testing.T) {
	other, err := NewSessions(strings.Repeat("x", 32), time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	mw := NewMiddleware(testSessions(t), fakeUserLookup{user: User{ID: "u1"}, ok: true})
	for name, cookie := range map[string]*http.Cookie{
		"legacy token":   {Name: SessionCookieName, Value: "J0CwBq7m0P1J8Yv3c8kxw0eYbYQ8j2zQ7Xn0d3a5b6c"},
		"foreign secret": sessionCookie(t, other, "u1"),
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(cookie)
		mw.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatalf("%s: handler should not run", name)
		})).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
	}
}

func TestRequireAuthStoresUserInContext(t *testing.T) {
	user := User{ID: "u1", Username: "jan", Role: RoleUser}
	sessions := testSessions(t)
	mw := NewMiddleware(sessions, fakeUserLookup{user: user, ok: true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookie(t, sessions, user.ID))

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

// A valid session whose user is gone signs nobody in.
func TestRequireAuthRejectsDeletedUser(t *testing.T) {
	sessions := testSessions(t)
	mw := NewMiddleware(sessions, fakeUserLookup{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookie(t, sessions, "gone"))

	mw.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireAdminRejectsRegularUser(t *testing.T) {
	mw := NewMiddleware(testSessions(t), fakeUserLookup{})
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

func TestFromOIDCKeepsEveryClaim(t *testing.T) {
	got := FromOIDC(kitauth.Claims{
		Subject: "s", PreferredUsername: "jan", Email: "j@x", EmailVerified: true, Name: "Jan M", Groups: []string{"admins"},
	})
	if got.Subject != "s" || got.Username != "jan" || got.Email != "j@x" || !got.EmailVerified || got.Name != "Jan M" ||
		len(got.Groups) != 1 || got.Groups[0] != "admins" {
		t.Fatalf("FromOIDC = %+v", got)
	}
}

type fakeUserLookup struct {
	user User
	ok   bool
	err  error
}

func (f fakeUserLookup) FindByID(context.Context, string) (User, bool, error) {
	return f.user, f.ok, f.err
}
