package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/store"
	kitauth "github.com/trick77/ship/kit/auth"
)

// The probes are kit's, answering a caller on this host only.
func TestHealth_returnsOK(t *testing.T) {
	srv := New(Deps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Fatalf("status = %d, body %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

// /api/model is public: shared pages render the context-% segment too, and
// their viewers are not signed in.
func TestModel_returnsChatModelFactsWithoutAuth(t *testing.T) {
	info := llm.ModelInfo{ID: "some-model", DisplayName: "Some Model", ContextWindow: 128000}
	srv := New(Deps{Model: info})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/model", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body llm.ModelInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body != info {
		t.Errorf("body = %+v, want %+v", body, info)
	}
}

func TestRecovery_turnsPanicInto500(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})
	h := middleware(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestLogging_skipsHealthEndpoint(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	h := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if strings.Contains(logs.String(), "http_request") {
		t.Fatalf("health request was logged: %s", logs.String())
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if !strings.Contains(logs.String(), "path=/api/me") {
		t.Fatalf("non-health request was not logged: %s", logs.String())
	}
}

func TestMeRequiresSession(t *testing.T) {
	srv := New(Deps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMeReturnsCurrentUser(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", DisplayName: "Jan", Role: auth.RoleAdmin, ResponseLanguage: "en"}
	srv := New(Deps{
		Auth:     auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{userID: user.ID},
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

func TestUpdateMeSetsResponseLanguage(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}
	srv := New(Deps{
		Auth:     auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{userID: user.ID},
		Users:    fakeUserStore{user: user, ok: true},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/me", strings.NewReader(`{"responseLanguage":"de"}`))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body auth.User
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ResponseLanguage != "de" {
		t.Fatalf("responseLanguage = %q, want de", body.ResponseLanguage)
	}
}

func TestUpdateMeRejectsUnsupportedLanguage(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}
	srv := New(Deps{
		Auth:     auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{userID: user.ID},
		Users:    fakeUserStore{user: user, ok: true},
	})
	// "auto" was removed as a value; both an unrelated tag and the retired
	// "auto" must be rejected now.
	for _, lang := range []string{"fr", "auto"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/me", strings.NewReader(`{"responseLanguage":"`+lang+`"}`))
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tok"})

		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("lang %q: status = %d, want 400: %s", lang, rec.Code, rec.Body.String())
		}
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

	server := newAuthenticatedServer(t, Deps{Artifacts: artifacts, UsersDir: usersDir})
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

func TestAdminUsersRequiresAdmin(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}
	srv := New(Deps{
		Auth:     auth.NewMiddleware(fakeSessionStore{userID: user.ID}, fakeUserStore{user: user, ok: true}),
		Sessions: fakeSessionStore{userID: user.ID},
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
		OIDC: fakeOIDCService{err: errors.New("bad callback")},
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

func TestAuthCallbackLogsFailureWithoutLeakingSecrets(t *testing.T) {
	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	urlErr := &url.Error{
		Op:  "Get",
		URL: "https://idp.example.com/token?client_secret=topsecret123&code=authcode456",
		Err: errors.New("400 Bad Request"),
	}
	wrapped := fmt.Errorf("exchange oidc code: %w", urlErr)

	srv := New(Deps{
		OIDC: fakeOIDCService{err: wrapped},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=x&code=authcode456", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?auth_error=oidc_callback_failed" {
		t.Fatalf("Location = %q", loc)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "oidc callback failed") {
		t.Fatalf("log output missing failure message: %s", logOutput)
	}
	if !strings.Contains(logOutput, "idp.example.com") {
		t.Fatalf("log output missing host, redaction too aggressive: %s", logOutput)
	}
	if strings.Contains(logOutput, "topsecret123") {
		t.Fatalf("log output leaked client_secret: %s", logOutput)
	}
	if strings.Contains(logOutput, "authcode456") {
		t.Fatalf("log output leaked auth code: %s", logOutput)
	}
}

func TestDevAuthLoginCreatesAdminSession(t *testing.T) {
	user := auth.User{ID: "u1", Username: "dev", Role: auth.RoleAdmin, ResponseLanguage: "en"}
	sessions, err := auth.NewSessions("0123456789abcdef0123456789abcdef", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Deps{
		Sessions: sessions,
		Users:    fakeUserStore{user: user},
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
	// The cookie is the session: it reads back as the user's loom id.
	next := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	next.AddCookie(cookie)
	if claims, ok := sessions.Read(next); !ok || claims.Subject != user.ID {
		t.Fatalf("session = %+v (ok %v), want subject %s", claims, ok, user.ID)
	}
}

// Logout clears the cookie, with the attributes the login set it with.
func TestAuthLogoutClearsTheSessionCookie(t *testing.T) {
	user := auth.User{ID: "u1", Username: "jan", Role: auth.RoleUser, ResponseLanguage: "en"}
	sessions, err := auth.NewSessions("0123456789abcdef0123456789abcdef", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Deps{
		Auth:     auth.NewMiddleware(sessions, fakeUserStore{user: user, ok: true}),
		Sessions: sessions,
		Users:    fakeUserStore{user: user, ok: true},
	})
	login := httptest.NewRecorder()
	if err := sessions.Issue(login, auth.SessionFor(user.ID)); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(login.Result().Cookies()[0])

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != auth.SessionCookieName || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
		t.Fatalf("cookies = %+v, want %s cleared", cleared, auth.SessionCookieName)
	}
}

func TestAuthLoginReturns503WhenOIDCDependencyMissing(t *testing.T) {
	srv := New(Deps{})
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
	claims kitauth.Claims
	err    error
}

func (f fakeOIDCService) Start(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://auth.example.com/authorize", http.StatusFound)
}

func (f fakeOIDCService) Callback(http.ResponseWriter, *http.Request) (kitauth.Claims, error) {
	return f.claims, f.err
}

// fakeSessionStore signs userID in for any request carrying a session cookie.
type fakeSessionStore struct {
	userID string
}

func (f fakeSessionStore) Read(r *http.Request) (kitauth.Claims, bool) {
	if c, err := r.Cookie(auth.SessionCookieName); err != nil || c.Value == "" || f.userID == "" {
		return kitauth.Claims{}, false
	}
	return auth.SessionFor(f.userID), true
}

func (f fakeSessionStore) Issue(w http.ResponseWriter, c kitauth.Claims) error {
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookieName, Value: "session-" + c.Subject})
	return nil
}

func (f fakeSessionStore) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookieName, MaxAge: -1})
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

func (f fakeUserStore) UpdateResponseLanguage(context.Context, string, string) error {
	return nil
}
