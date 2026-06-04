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
	"time"

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/mcp"
	"github.com/trick77/spark/internal/store"
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

func TestMCPStatusReturnsConfiguredServerCounts(t *testing.T) {
	srv := newAuthenticatedChatServer(t, Deps{
		MCP: fakeMCPService{statuses: []mcp.ServerStatus{
			{Name: "alpha", Active: true},
			{Name: "zeta", Active: false},
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/mcp/status", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body mcpStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Active != 1 || body.Configured != 2 {
		t.Fatalf("body = %#v, want active=1 configured=2", body)
	}
}

func TestDownloadArtifactRequiresOwningUser(t *testing.T) {
	usersDir := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	artifacts := artifact.NewStore(db)
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(usersDir, testUser.ID, "files", "outputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, testUser.ID, "files", "outputs", "report.txt"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := artifacts.Create(context.Background(), artifact.CreateInput{
		UserID:          testUser.ID,
		ThreadID:        "thread_1",
		DisplayFilename: "report.txt",
		VolumeRelPath:   "files/outputs/report.txt",
		MIMEType:        "text/plain; charset=utf-8",
		SizeBytes:       6,
	})
	if err != nil {
		t.Fatal(err)
	}

	server := newAuthenticatedChatServer(t, Deps{Artifacts: artifacts, UsersDir: usersDir})
	req := authenticatedRequest(http.MethodGet, "/api/artifacts/"+created.ID+"/download", "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "report" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "report.txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestOpenImageArtifactUsesSystemPreview(t *testing.T) {
	usersDir := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	artifacts := artifact.NewStore(db)
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO threads (id, user_id, title)
VALUES ('thread_1', 'user_1', 'Artifacts')`); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(usersDir, testUser.ID, "files", "outputs", "robot.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := artifacts.Create(context.Background(), artifact.CreateInput{
		UserID:          testUser.ID,
		ThreadID:        "thread_1",
		DisplayFilename: "robot.png",
		VolumeRelPath:   "files/outputs/robot.png",
		MIMEType:        "image/png",
		SizeBytes:       3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var opened string
	server := newAuthenticatedChatServer(t, Deps{
		Artifacts: artifacts,
		UsersDir:  usersDir,
		ArtifactOpener: artifactOpenerFunc(func(_ context.Context, path string) error {
			opened = path
			return nil
		}),
	})
	req := authenticatedRequest(http.MethodPost, "/api/artifacts/"+created.ID+"/open", "")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if opened != imagePath {
		t.Fatalf("opened = %q, want %q", opened, imagePath)
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

func TestAuthCallbackRedirectsOnOIDCError(t *testing.T) {
	srv := New(Deps{
		Version: "test",
		OIDC:    fakeOIDCService{err: errors.New("bad callback")},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=bad", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?auth_error=oidc_callback_failed" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestDevAuthLoginCreatesAdminSession(t *testing.T) {
	user := auth.User{ID: "u1", Username: "dev", Role: auth.RoleAdmin, ResponseLanguage: "auto"}
	session := auth.Session{Token: "tok", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	srv := New(Deps{
		Version: "test",
		Sessions: fakeSessionStore{
			session: session,
		},
		Users: fakeUserStore{user: user},
		DevAuthClaims: auth.Claims{
			Subject:  "dev-admin",
			Username: "dev",
			Email:    "dev@example.local",
			Name:     "Dev Admin",
			Groups:   []string{auth.DevAdminGroup},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != auth.SessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, auth.SessionCookieName)
	}
	if cookie.Value != "tok" {
		t.Fatalf("cookie value = %q, want tok", cookie.Value)
	}
}

func TestAuthLoginReturns503WhenOIDCDependencyMissing(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "oidc is not configured" {
		t.Fatalf("error = %q, want oidc is not configured", body["error"])
	}
}

type fakeOIDCService struct {
	claims auth.Claims
	err    error
}

func (f fakeOIDCService) StartLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://auth.example.com/authorize", http.StatusFound)
}

func (f fakeOIDCService) HandleCallback(*http.Request) (auth.Claims, error) {
	return f.claims, f.err
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
