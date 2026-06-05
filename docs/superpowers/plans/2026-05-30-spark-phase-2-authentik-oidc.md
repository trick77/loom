# Slop Phase 2 Authentik OIDC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add authentik-backed OIDC sign-in, Slop server-side sessions, role-aware API protection, and a frontend sign-in/authenticated shell.

**Architecture:** Slop acts as an OIDC relying party using `go-oidc` and `oauth2`, then issues its own opaque session cookie backed by SQLite. Authentik remains the identity source; Slop stores app-local users keyed by OIDC subject and maps admin role from the configured authentik group.

**Tech Stack:** Go 1.25, stdlib `net/http`, `github.com/coreos/go-oidc/v3/oidc`, `golang.org/x/oauth2`, pure-Go SQLite via `ncruces/go-sqlite3`, React 19, TypeScript, Tailwind v4, Vitest.

---

## File Structure

- Create `backend/internal/store/migrations/0002_auth_oidc.sql`: `users` and `sessions` schema.
- Create `backend/internal/auth/model.go`: user/session/claim types and context helpers.
- Create `backend/internal/auth/session.go`: session token generation, hashing, cookies, and SQLite session store.
- Create `backend/internal/auth/user_store.go`: upsert/list users by OIDC subject.
- Create `backend/internal/auth/oidc.go`: OIDC provider wrapper, state/nonce cookies, callback verification.
- Create `backend/internal/auth/middleware.go`: require-auth and require-admin middleware.
- Create tests beside each auth file.
- Modify `backend/internal/config/config.go` and tests: add OIDC env vars, remove required admin password.
- Modify `backend/internal/httpapi/server.go`: wire auth dependencies and routes.
- Create `backend/internal/httpapi/auth_handlers.go`: login/callback/logout/me/admin handlers.
- Modify `backend/cmd/slop/main.go`: pass DB/config into HTTP API and initialize OIDC auth.
- Modify `.env.example` and `compose.yaml`: replace admin password with authentik/OIDC config.
- Move `slop.png` to `frontend/src/assets/slop.png`.
- Create `frontend/src/api.ts`: typed API helpers for `/api/me`, logout, admin users.
- Modify `frontend/src/App.tsx`: signed-out screen, authenticated shell, admin view.
- Modify `frontend/src/App.test.tsx`: frontend auth state tests.
- Modify `README.md`: keep setup instructions aligned with implemented env names and behavior.

## Task 1: Configuration Contract

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `.env.example`
- Modify: `compose.yaml`

- [ ] **Step 1: Write failing config tests**

Add tests showing OIDC settings are loaded and `SLOP_ADMIN_INITIAL_PASSWORD` is no longer required:

```go
func TestLoad_defaultsDoNotRequireAdminPassword(t *testing.T) {
	t.Setenv("SLOP_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AdminInitialPassword != "" {
		t.Fatalf("AdminInitialPassword = %q, want empty legacy field", cfg.AdminInitialPassword)
	}
}

func TestLoad_oidcSettings(t *testing.T) {
	t.Setenv("SLOP_SESSION_SECRET", "test-secret")
	t.Setenv("SLOP_PUBLIC_URL", "https://slop.example.com")
	t.Setenv("SLOP_OIDC_ISSUER", "https://auth.example.com/application/o/slop/")
	t.Setenv("SLOP_OIDC_CLIENT_ID", "slop-client")
	t.Setenv("SLOP_OIDC_CLIENT_SECRET", "slop-secret")
	t.Setenv("SLOP_OIDC_REDIRECT_URL", "https://slop.example.com/api/auth/callback")
	t.Setenv("SLOP_OIDC_POST_LOGOUT_REDIRECT_URL", "https://slop.example.com/")
	t.Setenv("SLOP_OIDC_ADMIN_GROUP", "slop-admins")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://auth.example.com/application/o/slop/" {
		t.Fatalf("OIDC issuer = %q", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.AdminGroup != "slop-admins" {
		t.Fatalf("OIDC admin group = %q", cfg.OIDC.AdminGroup)
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `make test`

Expected: config tests fail because `Config.OIDC` does not exist and admin password is still required.

- [ ] **Step 3: Implement config changes**

Add:

```go
type OIDCConfig struct {
	Issuer                string
	ClientID              string
	ClientSecret          string
	RedirectURL           string
	PostLogoutRedirectURL string
	AdminGroup            string
}
```

Add `PublicURL string` and `OIDC OIDCConfig` to `Config`. Keep `AdminInitialPassword string` only if
needed for compatibility in tests, but do not require it. Load env vars exactly as named in the design.

- [ ] **Step 4: Update env files**

In `.env.example`, remove `SLOP_ADMIN_INITIAL_PASSWORD` and add:

```bash
SLOP_PUBLIC_URL=https://slop.example.com
SLOP_OIDC_ISSUER=https://auth.example.com/application/o/slop/
SLOP_OIDC_CLIENT_ID=
SLOP_OIDC_CLIENT_SECRET=
SLOP_OIDC_REDIRECT_URL=https://slop.example.com/api/auth/callback
SLOP_OIDC_POST_LOGOUT_REDIRECT_URL=https://slop.example.com/
SLOP_OIDC_ADMIN_GROUP=slop-admins
```

In `compose.yaml`, pass the same variables through to the `slop` service and remove
`SLOP_ADMIN_INITIAL_PASSWORD`.

- [ ] **Step 5: Verify and commit**

Run: `make test`

Expected: PASS.

Commit:

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go .env.example compose.yaml
git commit -m "feat: add oidc configuration"
```

## Task 2: Auth Database Schema

**Files:**
- Create: `backend/internal/store/migrations/0002_auth_oidc.sql`
- Modify: `backend/internal/store/store_test.go`

- [ ] **Step 1: Write failing migration test**

Extend `TestOpen_runsMigrations` to assert `users` and `sessions` exist and migration count is `2`:

```go
for _, table := range []string{"settings", "users", "sessions"} {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
		table,
	).Scan(&name)
	if err != nil {
		t.Fatalf("%s table missing: %v", table, err)
	}
}
```

Update both migration count assertions from `1` to `2`.

- [ ] **Step 2: Verify tests fail**

Run: `make test`

Expected: store tests fail because migration `0002` does not exist.

- [ ] **Step 3: Add migration**

Create `0002_auth_oidc.sql` with:

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    oidc_subject TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    response_language TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen_at TEXT
);

CREATE INDEX idx_users_role ON users(role);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
```

- [ ] **Step 4: Verify and commit**

Run: `make test`

Expected: PASS.

Commit:

```bash
git add backend/internal/store/migrations/0002_auth_oidc.sql backend/internal/store/store_test.go
git commit -m "feat: add auth database schema"
```

## Task 3: User Store

**Files:**
- Create: `backend/internal/auth/model.go`
- Create: `backend/internal/auth/user_store.go`
- Create: `backend/internal/auth/user_store_test.go`

- [ ] **Step 1: Write failing user store tests**

Create tests for upsert, role refresh, and listing:

```go
func TestUserStore_UpsertFromClaimsCreatesAndRefreshesRole(t *testing.T) {
	db := openTestDB(t)
	store := NewUserStore(db)

	claims := Claims{
		Subject: "authentik-sub-1",
		Username: "jan",
		Email: "jan@example.com",
		Name: "Jan",
		Groups: []string{"slop-admins"},
	}

	user, err := store.UpsertFromClaims(context.Background(), claims, "slop-admins")
	if err != nil {
		t.Fatalf("UpsertFromClaims() error: %v", err)
	}
	if user.Role != RoleAdmin {
		t.Fatalf("role = %q, want admin", user.Role)
	}

	claims.Groups = []string{"family"}
	user, err = store.UpsertFromClaims(context.Background(), claims, "slop-admins")
	if err != nil {
		t.Fatalf("second upsert error: %v", err)
	}
	if user.Role != RoleUser {
		t.Fatalf("role after refresh = %q, want user", user.Role)
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `cd backend && go test ./internal/auth`

Expected: package or symbols missing.

- [ ] **Step 3: Implement user model and store**

Define `Role`, `User`, `Claims`, `UserStore`, `UpsertFromClaims`, `ListUsers`, and deterministic role
mapping from `Claims.Groups`. Generate user IDs with `crypto/rand` URL-safe bytes.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd backend && go test ./internal/auth
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/auth/model.go backend/internal/auth/user_store.go backend/internal/auth/user_store_test.go
git commit -m "feat: add oidc user store"
```

## Task 4: Session Store and Cookies

**Files:**
- Create: `backend/internal/auth/session.go`
- Create: `backend/internal/auth/session_test.go`

- [ ] **Step 1: Write failing session tests**

Cover token hashing, lookup, expiry, revoke, and cookie flags:

```go
func TestSessionStore_CreateStoresOnlyHashAndFindsUser(t *testing.T) {
	db := openTestDB(t)
	user := insertTestUser(t, db, RoleUser)
	store := NewSessionStore(db, "test-secret", false)

	session, err := store.Create(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if session.Token == "" {
		t.Fatal("token is empty")
	}

	var rawCount int
	err = db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, session.Token).Scan(&rawCount)
	if err != nil {
		t.Fatalf("raw token query: %v", err)
	}
	if rawCount != 0 {
		t.Fatal("raw token was stored")
	}

	got, ok, err := store.Lookup(context.Background(), session.Token)
	if err != nil || !ok {
		t.Fatalf("Lookup() = ok %v err %v", ok, err)
	}
	if got.UserID != user.ID {
		t.Fatalf("user id = %q, want %q", got.UserID, user.ID)
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run: `cd backend && go test ./internal/auth`

Expected: session symbols missing.

- [ ] **Step 3: Implement session store**

Generate 32 random bytes, encode with `base64.RawURLEncoding`, hash with SHA-256 before storing.
Use cookie name `slop_session`. `CookieFor(token, expires)` returns `HttpOnly`, `SameSite=Lax`,
path `/`, and `Secure` based on config/runtime.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd backend && go test ./internal/auth
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/auth/session.go backend/internal/auth/session_test.go
git commit -m "feat: add server side sessions"
```

## Task 5: OIDC Provider and Callback Verification

**Files:**
- Create: `backend/internal/auth/oidc.go`
- Create: `backend/internal/auth/oidc_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

- [ ] **Step 1: Add dependencies**

Run:

```bash
cd backend && go get github.com/coreos/go-oidc/v3/oidc golang.org/x/oauth2
```

Expected: `go.mod` and `go.sum` update.

- [ ] **Step 2: Write failing OIDC tests**

Use test seams instead of a live authentik server. Define an interface for exchange/verify:

```go
func TestOIDCService_CallbackRejectsInvalidState(t *testing.T) {
	service := newTestOIDCService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=bad&code=abc", nil)

	_, err := service.HandleCallback(req)
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error = %v, want ErrInvalidState", err)
	}
}

func TestOIDCService_CallbackMapsVerifiedClaims(t *testing.T) {
	service := newTestOIDCServiceWithClaims(t, Claims{
		Subject: "sub-1",
		Username: "jan",
		Email: "jan@example.com",
		Groups: []string{"slop-admins"},
	})
	req := requestWithValidStateAndNonce(t)

	claims, err := service.HandleCallback(req)
	if err != nil {
		t.Fatalf("HandleCallback() error: %v", err)
	}
	if claims.Subject != "sub-1" {
		t.Fatalf("subject = %q", claims.Subject)
	}
}
```

- [ ] **Step 3: Verify tests fail**

Run: `cd backend && go test ./internal/auth`

Expected: OIDC service symbols missing.

- [ ] **Step 4: Implement OIDC service**

Use `oidc.NewProvider(ctx, cfg.Issuer)`, `provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})`,
`oauth2.Config`, `AuthCodeURL(state, oidc.Nonce(nonce))`, `Exchange`, raw `id_token` extraction,
`verifier.Verify`, nonce comparison, and custom claim extraction into `Claims`.

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd backend && go test ./internal/auth
make test
```

Expected: PASS.

Commit:

```bash
git add backend/go.mod backend/go.sum backend/internal/auth/oidc.go backend/internal/auth/oidc_test.go
git commit -m "feat: verify authentik oidc callbacks"
```

## Task 6: Auth Middleware

**Files:**
- Create: `backend/internal/auth/middleware.go`
- Create: `backend/internal/auth/middleware_test.go`

- [ ] **Step 1: Write failing middleware tests**

Cover unauthenticated `401`, authenticated context injection, and non-admin `403`:

```go
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
```

- [ ] **Step 2: Verify tests fail**

Run: `cd backend && go test ./internal/auth`

Expected: middleware symbols missing.

- [ ] **Step 3: Implement middleware**

Read `slop_session`, look up active session, load user, attach `User` to context. `RequireAdmin`
checks `RoleAdmin`. Return JSON errors with `Content-Type: application/json`.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd backend && go test ./internal/auth
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/auth/middleware.go backend/internal/auth/middleware_test.go
git commit -m "feat: add auth middleware"
```

## Task 7: HTTP Auth API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Create: `backend/internal/httpapi/auth_handlers.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/cmd/slop/main.go`

- [ ] **Step 1: Write failing HTTP API tests**

Add tests for `/api/me`, `/api/auth/logout`, and `/api/admin/users`:

```go
func TestMeRequiresSession(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
```

Add a second test with fake auth dependencies returning a user and assert JSON contains `role`.

- [ ] **Step 2: Verify tests fail**

Run: `make test`

Expected: route missing or dependency shape missing.

- [ ] **Step 3: Implement routes and dependency wiring**

Extend `httpapi.Deps` with auth service, session store, user store, and middleware. Register:

```go
mux.HandleFunc("GET /api/auth/login", s.handleAuthLogin)
mux.HandleFunc("GET /api/auth/callback", s.handleAuthCallback)
mux.Handle("POST /api/auth/logout", authMW.RequireAuth(http.HandlerFunc(s.handleAuthLogout)))
mux.Handle("GET /api/me", authMW.RequireAuth(http.HandlerFunc(s.handleMe)))
mux.Handle("GET /api/admin/users", authMW.RequireAuth(authMW.RequireAdmin(http.HandlerFunc(s.handleAdminUsers))))
```

Update `cmd/slop/main.go` to initialize the auth stores from `db` and `cfg`, then pass them to
`httpapi.New`.

- [ ] **Step 4: Verify and commit**

Run: `make test`

Expected: PASS.

Commit:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/auth_handlers.go backend/internal/httpapi/server_test.go backend/cmd/slop/main.go
git commit -m "feat: expose oidc auth api"
```

## Task 8: Frontend Auth States

**Files:**
- Move: `slop.png` to `frontend/src/assets/slop.png`
- Create: `frontend/src/api.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Move Slop image**

Run:

```bash
mkdir -p frontend/src/assets
mv slop.png frontend/src/assets/slop.png
```

- [ ] **Step 2: Write failing frontend tests**

Mock `fetch` and cover signed-out, signed-in, and admin states:

```tsx
test("renders signed-out screen when /api/me returns 401", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 401 })));

  render(<App />);

  expect(await screen.findByRole("link", { name: /sign in/i })).toHaveAttribute(
    "href",
    "/api/auth/login",
  );
  expect(screen.getByAltText("Slop")).toBeInTheDocument();
});

test("renders admin entry for admin users", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({ id: "u1", username: "jan", role: "admin", displayName: "Jan" }),
    ),
  );

  render(<App />);

  expect(await screen.findByRole("button", { name: /admin/i })).toBeInTheDocument();
});
```

- [ ] **Step 3: Verify tests fail**

Run: `make fe-test`

Expected: tests fail because auth states are not implemented.

- [ ] **Step 4: Implement frontend API and states**

`api.ts` exports `getMe`, `logout`, and `listUsers`. `App.tsx` renders loading, signed-out,
error, authenticated shell, and admin user list. Logout posts to `/api/auth/logout` and navigates to
the returned `redirectUrl` or `/`.

- [ ] **Step 5: Verify and commit**

Run:

```bash
make fe-test
make fe-build
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
```

Expected: frontend tests and build pass; built assets are restored to placeholders.

Commit:

```bash
git add frontend/src/assets/slop.png frontend/src/api.ts frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "feat: add oidc frontend auth states"
```

## Task 9: README and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-05-30-slop-phase-2-authentik-oidc-design.md` only if implementation diverged.

- [ ] **Step 1: Review README against implementation**

Ensure README env names, routes, callback URL, logout behavior, group claim expectation, and smoke
test match the implemented code.

- [ ] **Step 2: Run full verification**

Run:

```bash
make test
make fe-test
make fe-build
make build
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
git status --short
```

Expected:

- Backend tests pass.
- Frontend tests pass.
- Frontend build passes.
- Full binary build passes.
- `backend/web/dist/.gitkeep` and `backend/web/dist/index.html` are not modified.
- Only intended source/docs files are changed.

- [ ] **Step 3: Commit docs alignment if needed**

If README or design docs changed:

```bash
git add README.md docs/superpowers/specs/2026-05-30-slop-phase-2-authentik-oidc-design.md
git commit -m "docs: align authentik setup with implementation"
```

- [ ] **Step 4: Handoff**

Summarize commits, verification output, and any manual authentik configuration still required for a
real smoke test.

## Self-Review

- Spec coverage: OIDC login, local sessions, role mapping, frontend signed-out/authenticated states,
  `slop.png`, README setup, and admin user list are all covered.
- Placeholder scan: no red-flag placeholder terms remain.
- Type consistency: config uses `OIDCConfig`; backend auth exposes `User`, `Claims`, role constants,
  user/session stores, OIDC service, and middleware used by HTTP handlers.
- Scope check: local password auth and authentik user lifecycle remain excluded, matching the design.
