# lume Phase 1 — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a runnable lume foundation — a single Go binary that serves a JSON/SSE API and an embedded React/Vite/Tailwind frontend, backed by a SQLite store (with sqlite-vec verified), config from ENV, and a runtime-agnostic container/compose skeleton.

**Architecture:** One Go module (`backend/`) serves `/api/*` (JSON + SSE) and the embedded React build (`web/dist` via `embed.FS`) with SPA fallback. Persistence is one SQLite file accessed through the **pure-Go** `ncruces/go-sqlite3` driver (no cgo) with **sqlite-vec** linked via `sqlite-vec-go-bindings/ncruces`. A hand-rolled migration runner applies embedded `.sql` files. The frontend is a Vite + React + TypeScript + Tailwind app using the chosen **UI direction A (Warm Editorial)** palette. Containerfile is multi-stage (Node build → Go static build → distroless), `compose.yaml` wires `slopr` + `searxng` + `tika`.

**Tech Stack:** Go 1.25 (stdlib `net/http` 1.22+ routing, no web framework), `github.com/ncruces/go-sqlite3` + `github.com/asg017/sqlite-vec-go-bindings/ncruces`, React 18 + TypeScript + Vite 5 + Tailwind 3, Vitest + Testing Library, OCI Containerfile/compose (Podman/Docker).

---

## Decisions locked in this phase

- **SQLite driver:** pure-Go `ncruces/go-sqlite3` (WASM/wazero) via its `database/sql` driver. `CGO_ENABLED=0` everywhere.
- **sqlite-vec:** `github.com/asg017/sqlite-vec-go-bindings/ncruces` (blank import in the `store` package provides the WASM build with sqlite-vec compiled in — replaces `ncruces/go-sqlite3/embed`).
- **Routing:** stdlib `http.ServeMux` with Go 1.22+ method/pattern routes; hand-rolled middleware. No chi/gin.
- **Migrations:** hand-rolled runner over `embed.FS` `.sql` files, tracked in `schema_migrations`.
- **Module path:** `github.com/trick77/lume` (adjust if the remote differs).
- **Fonts:** wire a CSS-variable font system; ship open stand-ins via `@fontsource` (`@fontsource-variable/inter` for sans, `@fontsource/fraunces` for the editorial serif) with a single documented swap point for the real Anthropic font package when its npm name is confirmed.
- **Branch:** continue on a feature branch off `design/slopr-v1` (e.g. `feat/phase-1-foundation`); never commit to `master`.

## File Structure

```
slopr/
  backend/
    go.mod                                  # module github.com/trick77/lume, go 1.25
    cmd/slopr/main.go                         # entrypoint: load config, open store, build server, serve + graceful shutdown
    internal/
      config/config.go                      # Config struct + Load() from ENV
      config/config_test.go
      store/store.go                        # Open(dsn) -> *sql.DB, PRAGMAs, runs migrations
      store/migrate.go                      # migration runner over embedded .sql
      store/store_test.go                   # store open + migration idempotency
      store/vec_test.go                     # sqlite-vec smoke test (vec_version + vec0 KNN)
      store/migrations/0001_init.sql        # settings table
      httpapi/server.go                     # New(...) http.Handler: routes + static + middleware
      httpapi/health.go                     # GET /api/health, GET /api/health/stream (SSE demo)
      httpapi/middleware.go                 # logging + recovery
      httpapi/server_test.go                # health + SPA fallback + middleware tests
      sse/sse.go                            # SSE writer helper
      sse/sse_test.go
    web/embed.go                            # //go:embed all:dist ; DistFS() + SPA handler
    web/dist/.gitkeep                        # keeps embed compilable before a frontend build
    Containerfile
    .dockerignore
  frontend/
    package.json  tsconfig.json  vite.config.ts  tailwind.config.ts  postcss.config.js
    index.html
    vitest.config.ts  vitest.setup.ts
    src/main.tsx  src/App.tsx  src/index.css
    src/theme/tokens.css                    # UI direction A palette as CSS variables
    src/App.test.tsx
  compose.yaml
  mcp.json                                   # example MCP server config (HTTP/SSE)
  .env.example
  Makefile
  .gitignore                                 # (exists) — extend for backend/web/dist build output
```

---

## Task 1: Backend Go module & scaffold

**Files:**
- Create: `backend/go.mod`, `backend/cmd/slopr/main.go` (temporary stub), `Makefile`

- [ ] **Step 1: Create the Go module**

Run:
```bash
mkdir -p backend/cmd/slopr && cd backend && go mod init github.com/trick77/lume && go mod edit -go=1.25
```

- [ ] **Step 2: Add a temporary main stub so the module compiles**

Create `backend/cmd/slopr/main.go`:
```go
package main

import "fmt"

func main() {
	fmt.Println("slopr")
}
```

- [ ] **Step 3: Verify it builds**

Run: `cd backend && go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Add a Makefile at repo root**

Create `Makefile`:
```makefile
.PHONY: build test fe-build fe-test run tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

fe-test:
	cd frontend && npm run test -- --run

fe-build:
	cd frontend && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -o ../bin/slopr ./cmd/slopr

run:
	cd backend && go run ./cmd/slopr
```

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/cmd/slopr/main.go Makefile
git commit -m "chore: scaffold Go module and Makefile"
```

---

## Task 2: Config package

**Files:**
- Create: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/config_test.go`:
```go
package config

import "testing"

func TestLoad_defaults(t *testing.T) {
	t.Setenv("SLOPR_SESSION_SECRET", "test-secret")
	t.Setenv("SLOPR_ADMIN_INITIAL_PASSWORD", "admin-pw")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/slopr.db" {
		t.Errorf("DBPath default = %q, want /data/slopr.db", cfg.DBPath)
	}
	if cfg.UsersDir != "/data/users" {
		t.Errorf("UsersDir default = %q, want /data/users", cfg.UsersDir)
	}
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("SLOPR_ADDR", ":9000")
	t.Setenv("SLOPR_SESSION_SECRET", "")
	t.Setenv("SLOPR_ADMIN_INITIAL_PASSWORD", "admin-pw")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when SLOPR_SESSION_SECRET is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/config/...`
Expected: FAIL — `config.Load` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/config/config.go`:
```go
// Package config loads slopr's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime settings. Secrets come from ENV only.
type Config struct {
	Addr     string // HTTP listen address
	DBPath   string // path to the SQLite file
	UsersDir string // root for per-user volumes: <UsersDir>/<user-id>/

	ChatBaseURL  string // OpenAI-compatible chat endpoint (MiMo)
	ChatAPIKey   string
	ChatModel    string
	EmbedBaseURL string // OpenAI embeddings endpoint
	EmbedAPIKey  string
	EmbedModel   string

	TikaURL       string
	SearxngURL    string
	MCPConfigPath string

	AdminInitialPassword string
	SessionSecret        string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		Addr:                 env("SLOPR_ADDR", ":8080"),
		DBPath:               env("SLOPR_DB_PATH", "/data/slopr.db"),
		UsersDir:             env("SLOPR_USERS_DIR", "/data/users"),
		ChatBaseURL:          env("SLOPR_CHAT_BASE_URL", ""),
		ChatAPIKey:           env("SLOPR_CHAT_API_KEY", ""),
		ChatModel:            env("SLOPR_CHAT_MODEL", "MiMo"),
		EmbedBaseURL:         env("SLOPR_EMBED_BASE_URL", ""),
		EmbedAPIKey:          env("SLOPR_EMBED_API_KEY", ""),
		EmbedModel:           env("SLOPR_EMBED_MODEL", "text-embedding-3-small"),
		TikaURL:              env("SLOPR_TIKA_URL", "http://tika:9998"),
		SearxngURL:           env("SLOPR_SEARXNG_URL", "http://searxng:8080"),
		MCPConfigPath:        env("SLOPR_MCP_CONFIG", "/config/mcp.json"),
		AdminInitialPassword: env("SLOPR_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:        env("SLOPR_SESSION_SECRET", ""),
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SLOPR_SESSION_SECRET is required")
	}
	if cfg.AdminInitialPassword == "" {
		return Config{}, fmt.Errorf("SLOPR_ADMIN_INITIAL_PASSWORD is required")
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/
git commit -m "feat(config): load runtime config from environment"
```

---

## Task 3: SQLite store & migration runner

**Files:**
- Create: `backend/internal/store/store.go`, `backend/internal/store/migrate.go`, `backend/internal/store/migrations/0001_init.sql`, `backend/internal/store/store_test.go`

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd backend && go get github.com/ncruces/go-sqlite3@latest && go get github.com/asg017/sqlite-vec-go-bindings/ncruces@latest && go mod tidy
```

- [ ] **Step 2: Write the failing test**

Create `backend/internal/store/store_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestOpen_runsMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	// settings table from 0001 must exist
	var name string
	err = db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='settings'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("settings table missing: %v", err)
	}

	// migrations are tracked
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != 1 {
		t.Errorf("applied migrations = %d, want 1", count)
	}
}

func TestOpen_migrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	db1.Close()

	db2, err := Open(dbPath) // re-open: must not re-apply or error
	if err != nil {
		t.Fatalf("second Open() error: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != 1 {
		t.Errorf("applied migrations after re-open = %d, want 1", count)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/...`
Expected: FAIL — `store.Open` undefined.

- [ ] **Step 4: Create the first migration**

Create `backend/internal/store/migrations/0001_init.sql`:
```sql
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

- [ ] **Step 5: Write the store implementation**

Create `backend/internal/store/store.go`:
```go
// Package store opens the SQLite database (pure-Go ncruces driver with
// sqlite-vec linked in) and applies embedded migrations.
package store

import (
	"database/sql"
	"fmt"
	"net/url"

	// sqlite-vec WASM build for ncruces; provides the SQLite WASM binary AND
	// the vec0 virtual table + vec_* functions. Replaces ncruces/go-sqlite3/embed.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	// registers the "sqlite3" database/sql driver.
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Open opens (creating if needed) the SQLite database at path, applies PRAGMAs
// for safe concurrent use, runs migrations, and returns the *sql.DB.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(wal)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(on)",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
```

- [ ] **Step 6: Write the migration runner**

Create `backend/internal/store/migrate.go`:
```go
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate applies any embedded migrations not yet recorded in schema_migrations,
// each in its own transaction, in lexicographic filename order.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var dummy int
		err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, name).Scan(&dummy)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return err
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS (both tests).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/store/ backend/go.mod backend/go.sum
git commit -m "feat(store): sqlite (ncruces) store with embedded migration runner"
```

---

## Task 4: sqlite-vec smoke verification (verify early)

**Files:**
- Create: `backend/internal/store/vec_test.go`

This is the spec's "verify `sqlite-vec` early" gate: confirm the chosen pure-Go driver exposes `vec_version()` and a working `vec0` KNN query before later phases depend on it.

- [ ] **Step 1: Write the test**

Create `backend/internal/store/vec_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestSqliteVec_versionAndKNN(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vec.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	// vec_version() proves the extension is linked.
	var ver string
	if err := db.QueryRow(`SELECT vec_version()`).Scan(&ver); err != nil {
		t.Fatalf("vec_version() error: %v", err)
	}
	if ver == "" {
		t.Fatal("vec_version() returned empty string")
	}

	// Create a vec0 table, insert 3 vectors, run a KNN query.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE v USING vec0(embedding float[3])`); err != nil {
		t.Fatalf("create vec0: %v", err)
	}
	rows := [][2]any{
		{int64(1), "[1.0, 0.0, 0.0]"},
		{int64(2), "[0.0, 1.0, 0.0]"},
		{int64(3), "[0.9, 0.1, 0.0]"},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO v(rowid, embedding) VALUES (?, ?)`, r[0], r[1]); err != nil {
			t.Fatalf("insert vector: %v", err)
		}
	}

	var nearest int64
	err = db.QueryRow(`
		SELECT rowid FROM v
		WHERE embedding MATCH ? AND k = 1
		ORDER BY distance`, "[1.0, 0.0, 0.0]").Scan(&nearest)
	if err != nil {
		t.Fatalf("KNN query: %v", err)
	}
	if nearest != 1 {
		t.Errorf("nearest rowid = %d, want 1", nearest)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `cd backend && go test ./internal/store/ -run TestSqliteVec -v`
Expected: PASS. (If it FAILS, the pure-Go path is not viable on this platform — stop and fall back to the cgo path: `mattn/go-sqlite3` + `sqlite-vec-go-bindings/cgo` with `sqlite_vec.Auto()`, and set `CGO_ENABLED=1` + a glibc/musl base image in Task 10. Record the switch in this plan before continuing.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/store/vec_test.go
git commit -m "test(store): verify sqlite-vec vec_version and vec0 KNN"
```

---

## Task 5: HTTP server, health endpoint & middleware

**Files:**
- Create: `backend/internal/httpapi/server.go`, `backend/internal/httpapi/health.go`, `backend/internal/httpapi/middleware.go`, `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/httpapi/server_test.go`:
```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/...`
Expected: FAIL — `New`, `Deps`, `recovery` undefined.

- [ ] **Step 3: Write the middleware**

Create `backend/internal/httpapi/middleware.go`:
```go
package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// recovery converts panics in downstream handlers into 500 responses.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "err", rec, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logging logs each request with method, path, and duration.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).String())
	})
}
```

- [ ] **Step 4: Write the health handlers**

Create `backend/internal/httpapi/health.go`:
```go
package httpapi

import (
	"encoding/json"
	"net/http"
)

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}
```

- [ ] **Step 5: Write the server**

Create `backend/internal/httpapi/server.go`:
```go
// Package httpapi builds slopr's HTTP handler: JSON/SSE API plus the embedded SPA.
package httpapi

import "net/http"

// Deps are the dependencies needed to build the server. Grows in later phases
// (store, config, services); for Phase 1 only Version and the static handler.
type Deps struct {
	Version string
	Static  http.Handler // serves the embedded SPA; may be nil in tests
}

type server struct {
	version string
}

// New returns the fully wired HTTP handler.
func New(d Deps) http.Handler {
	s := &server{version: d.Version}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/stream", s.handleHealthStream)
	if d.Static != nil {
		mux.Handle("/", d.Static)
	}

	return logging(recovery(mux))
}
```

- [ ] **Step 6: Add a temporary stub for the SSE handler (implemented in Task 6)**

Append to `backend/internal/httpapi/health.go`:
```go
// handleHealthStream is implemented in Task 6 (sse). Temporary stub so the
// package compiles.
func (s *server) handleHealthStream(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./internal/httpapi/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/httpapi/
git commit -m "feat(httpapi): server with health endpoint, logging and recovery middleware"
```

---

## Task 6: SSE helper & live health stream

**Files:**
- Create: `backend/internal/sse/sse.go`, `backend/internal/sse/sse_test.go`
- Modify: `backend/internal/httpapi/health.go` (replace the stub)

- [ ] **Step 1: Write the failing test**

Create `backend/internal/sse/sse_test.go`:
```go
package sse

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriter_writesEventAndData(t *testing.T) {
	rec := httptest.NewRecorder()

	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter error: %v", err)
	}
	if err := w.Send("ping", `{"n":1}`); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: ping\n") {
		t.Errorf("missing event line in %q", out)
	}
	if !strings.Contains(out, "data: {\"n\":1}\n\n") {
		t.Errorf("missing data line in %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/sse/...`
Expected: FAIL — `sse.NewWriter` undefined.

- [ ] **Step 3: Write the SSE writer**

Create `backend/internal/sse/sse.go`:
```go
// Package sse provides a minimal Server-Sent Events writer.
package sse

import (
	"fmt"
	"net/http"
)

// Writer streams SSE events to an http.ResponseWriter.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter sets SSE headers and returns a Writer, or an error if the
// ResponseWriter does not support flushing.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &Writer{w: w, flusher: flusher}, nil
}

// Send writes one event with the given name and data payload, then flushes.
func (s *Writer) Send(event, data string) error {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/sse/...`
Expected: PASS.

- [ ] **Step 5: Replace the health-stream stub with a real SSE handler**

In `backend/internal/httpapi/health.go`, replace the `handleHealthStream` stub with:
```go
// handleHealthStream emits a few SSE events to exercise the streaming path.
func (s *server) handleHealthStream(w http.ResponseWriter, r *http.Request) {
	stream, err := sse.NewWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := 1; i <= 3; i++ {
		select {
		case <-r.Context().Done():
			return
		default:
			_ = stream.Send("tick", fmt.Sprintf(`{"n":%d}`, i))
		}
	}
}
```
Add the imports `"fmt"` and `"github.com/trick77/lume/internal/sse"` to `health.go`.

- [ ] **Step 6: Run tests to verify everything passes**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/sse/ backend/internal/httpapi/health.go
git commit -m "feat(sse): SSE writer helper and live health stream endpoint"
```

---

## Task 7: Embedded SPA serving

**Files:**
- Create: `backend/web/embed.go`, `backend/web/dist/.gitkeep`, `backend/web/dist/index.html` (temporary), `backend/web/embed_test.go`
- Modify: `.gitignore`

- [ ] **Step 1: Create the dist placeholder so embed compiles**

```bash
mkdir -p backend/web/dist
printf '' > backend/web/dist/.gitkeep
printf '<!doctype html><title>slopr</title><div id="root"></div>' > backend/web/dist/index.html
```

- [ ] **Step 2: Extend `.gitignore` to ignore built assets but keep the dir**

Append to `.gitignore`:
```
# Embedded frontend build output (generated; keep dir + placeholders)
backend/web/dist/*
!backend/web/dist/.gitkeep
!backend/web/dist/index.html
```

- [ ] **Step 3: Write the failing test**

Create `backend/web/embed_test.go`:
```go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSPAHandler_servesIndexFallback(t *testing.T) {
	h := SPAHandler()

	// An unknown client-side route must fall back to index.html (200).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/123", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("fallback status = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./web/...`
Expected: FAIL — `web.SPAHandler` undefined.

- [ ] **Step 5: Write the embed + SPA handler**

Create `backend/web/embed.go`:
```go
// Package web embeds the built frontend (web/dist) and serves it as a SPA.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// SPAHandler serves the embedded frontend. Existing files are served directly;
// any other path falls back to index.html so client-side routing works.
func SPAHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time; this is a programmer error
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./web/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/web/ .gitignore
git commit -m "feat(web): embed and serve frontend dist as a SPA"
```

---

## Task 8: Wire everything in main.go

**Files:**
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Replace the stub with real wiring**

Replace `backend/cmd/slopr/main.go` with:
```go
// Command slopr is the all-in-one server: API + embedded SPA, backed by SQLite.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/trick77/lume/internal/config"
	"github.com/trick77/lume/internal/httpapi"
	"github.com/trick77/lume/internal/store"
	"github.com/trick77/lume/web"
)

var version = "dev" // overridden via -ldflags at build time

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	handler := httpapi.New(httpapi.Deps{
		Version: version,
		Static:  web.SPAHandler(),
	})

	srv := &http.Server{Addr: cfg.Addr, Handler: handler}

	go func() {
		slog.Info("listening", "addr", cfg.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 2: Verify it builds and the full test suite passes**

Run: `cd backend && go build ./... && go test ./...`
Expected: build OK; all tests PASS.

- [ ] **Step 3: Manual smoke test**

Run:
```bash
cd backend && SLOPR_SESSION_SECRET=dev SLOPR_ADMIN_INITIAL_PASSWORD=dev SLOPR_DB_PATH=/tmp/slopr.db go run ./cmd/slopr &
sleep 1
curl -s localhost:8080/api/health
curl -s localhost:8080/api/health/stream
kill %1
```
Expected: health returns `{"status":"ok","version":"dev"}`; stream prints three `tick` events.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/slopr/main.go
git commit -m "feat: wire config, store and HTTP server in main"
```

---

## Task 9: Frontend scaffold (Vite + React + TS + Tailwind, UI direction A)

**Files:**
- Create: `frontend/package.json`, `frontend/tsconfig.json`, `frontend/vite.config.ts`, `frontend/tailwind.config.ts`, `frontend/postcss.config.js`, `frontend/index.html`, `frontend/vitest.config.ts`, `frontend/vitest.setup.ts`, `frontend/src/main.tsx`, `frontend/src/App.tsx`, `frontend/src/index.css`, `frontend/src/theme/tokens.css`, `frontend/src/App.test.tsx`
- Modify: `.gitignore` (already ignores `node_modules/`, `frontend/dist/`)

- [ ] **Step 1: Initialize the frontend package**

Run:
```bash
mkdir -p frontend/src/theme && cd frontend
npm init -y
npm install react react-dom
npm install -D vite @vitejs/plugin-react typescript @types/react @types/react-dom \
  tailwindcss postcss autoprefixer \
  vitest @testing-library/react @testing-library/jest-dom jsdom \
  @fontsource-variable/inter @fontsource/fraunces
```

- [ ] **Step 2: Add scripts to `frontend/package.json`**

Set the `"scripts"` block in `frontend/package.json` to:
```json
{
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest"
  }
}
```

- [ ] **Step 3: Create config files**

Create `frontend/vite.config.ts`:
```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": "http://localhost:8080" } },
  build: {
    outDir: path.resolve(__dirname, "../backend/web/dist"),
    emptyOutDir: true,
  },
});
```

Create `frontend/vitest.config.ts`:
```ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: { environment: "jsdom", globals: true, setupFiles: ["./vitest.setup.ts"] },
});
```

Create `frontend/vitest.setup.ts`:
```ts
import "@testing-library/jest-dom/vitest";
```

Create `frontend/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noEmit": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"]
}
```

Create `frontend/postcss.config.js`:
```js
export default { plugins: { tailwindcss: {}, autoprefixer: {} } };
```

Create `frontend/tailwind.config.ts`:
```ts
import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: "var(--slopr-bg)",
        panel: "var(--slopr-panel)",
        active: "var(--slopr-active)",
        border: "var(--slopr-border)",
        ink: "var(--slopr-text)",
        muted: "var(--slopr-muted)",
        accent: "var(--slopr-accent)",
      },
      fontFamily: {
        sans: ["var(--slopr-font-sans)", "ui-sans-serif", "system-ui", "sans-serif"],
        serif: ["var(--slopr-font-serif)", "Georgia", "serif"],
      },
      borderRadius: { slopr: "12px" },
    },
  },
  plugins: [],
} satisfies Config;
```

- [ ] **Step 4: Create the theme tokens (UI direction A — Warm Editorial)**

Create `frontend/src/theme/tokens.css`:
```css
/* UI direction A — Warm Editorial (Claude-inspired). Starting palette from the
   design spec; fine-tuned during the frontend phase. */
:root {
  --slopr-bg: #faf7f2;
  --slopr-panel: #f1ebe1;
  --slopr-active: #e9dfce;
  --slopr-border: #e3dccf;
  --slopr-text: #2b2520;
  --slopr-muted: #9a8f7e;
  --slopr-accent: #cc785c;
  --slopr-accent-text: #ffffff;

  /* Font stand-ins. SWAP POINT: replace these two custom-props (and the
     @fontsource imports in index.css) with the real Anthropic font package
     once its npm name is confirmed. */
  --slopr-font-sans: "Inter Variable";
  --slopr-font-serif: "Fraunces";
}
```

Create `frontend/src/index.css`:
```css
@import "@fontsource-variable/inter";
@import "@fontsource/fraunces";
@import "./theme/tokens.css";

@tailwind base;
@tailwind components;
@tailwind utilities;

body {
  margin: 0;
  background: var(--slopr-bg);
  color: var(--slopr-text);
  font-family: var(--slopr-font-sans), ui-sans-serif, system-ui, sans-serif;
}
```

- [ ] **Step 5: Create `index.html`, `main.tsx`, and the App shell**

Create `frontend/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>slopr</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

Create `frontend/src/main.tsx`:
```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

Create `frontend/src/App.tsx` (minimal three-column shell from the spec):
```tsx
export default function App() {
  return (
    <div className="grid h-screen grid-cols-[240px_1fr_300px] font-sans text-ink">
      <aside className="flex flex-col gap-2 bg-panel p-3 border-r border-border">
        <div className="font-serif text-xl font-semibold">slopr</div>
        <button className="rounded-slopr bg-accent px-3 py-2 text-sm text-white">
          + New chat
        </button>
      </aside>
      <main className="flex flex-col bg-bg p-6">
        <h1 className="font-serif text-lg">Welcome to slopr</h1>
        <p className="text-muted">Foundation is up. Chat arrives in a later phase.</p>
      </main>
      <aside className="bg-panel border-l border-border p-3 text-sm text-muted">
        Context panel
      </aside>
    </div>
  );
}
```

- [ ] **Step 6: Write a Vitest component test**

Create `frontend/src/App.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import App from "./App";

test("renders the slopr brand and new chat action", () => {
  render(<App />);
  expect(screen.getByText("slopr")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /new chat/i })).toBeInTheDocument();
});
```

- [ ] **Step 7: Run the frontend test**

Run: `cd frontend && npm run test -- --run`
Expected: 1 passing test.

- [ ] **Step 8: Verify the production build outputs into the Go embed dir**

Run: `cd frontend && npm run build`
Expected: build succeeds; `backend/web/dist/index.html` + `backend/web/dist/assets/*` exist.

- [ ] **Step 9: Commit**

```bash
git add frontend/ .gitignore
git commit -m "feat(frontend): Vite+React+Tailwind scaffold with UI direction A and Vitest"
```

---

## Task 10: Container & compose skeleton

**Files:**
- Create: `backend/Containerfile`, `backend/.dockerignore`, `compose.yaml`, `mcp.json`, `.env.example`

- [ ] **Step 1: Create the multi-stage Containerfile**

Create `backend/Containerfile` (build context = repo root):
```dockerfile
# syntax=docker/dockerfile:1

# --- Stage 1: build the frontend ---
FROM node:22-alpine AS web
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Vite outDir is ../backend/web/dist (see vite.config.ts)
RUN npm run build

# --- Stage 2: build the Go binary (pure-Go, no cgo) ---
FROM golang:1.25-alpine AS build
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# bring in the built frontend so //go:embed all:dist has real assets
COPY --from=web /app/backend/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(date +%Y%m%d)" -o /out/slopr ./cmd/slopr

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/slopr /slopr
EXPOSE 8080
ENTRYPOINT ["/slopr"]
```

- [ ] **Step 2: Create `.dockerignore`**

Create `backend/.dockerignore` (at repo root, named `.dockerignore`):
```
**/node_modules
**/.git
bin
.superpowers
backend/web/dist/assets
```
> Note: place this file at the **repo root** as `.dockerignore` (build context is the repo root). Keep `backend/web/dist/.gitkeep` so the COPY target exists even if the web stage is skipped.

- [ ] **Step 3: Create `compose.yaml`**

Create `compose.yaml` at repo root:
```yaml
services:
  slopr:
    build:
      context: .
      dockerfile: backend/Containerfile
    ports:
      - "8080:8080"
    environment:
      SLOPR_ADDR: ":8080"
      SLOPR_DB_PATH: "/data/slopr.db"
      SLOPR_USERS_DIR: "/data/users"
      SLOPR_TIKA_URL: "http://tika:9998"
      SLOPR_SEARXNG_URL: "http://searxng:8080"
      SLOPR_MCP_CONFIG: "/config/mcp.json"
      SLOPR_CHAT_BASE_URL: "${SLOPR_CHAT_BASE_URL}"
      SLOPR_CHAT_API_KEY: "${SLOPR_CHAT_API_KEY}"
      SLOPR_CHAT_MODEL: "${SLOPR_CHAT_MODEL:-MiMo}"
      SLOPR_EMBED_BASE_URL: "${SLOPR_EMBED_BASE_URL}"
      SLOPR_EMBED_API_KEY: "${SLOPR_EMBED_API_KEY}"
      SLOPR_ADMIN_INITIAL_PASSWORD: "${SLOPR_ADMIN_INITIAL_PASSWORD}"
      SLOPR_SESSION_SECRET: "${SLOPR_SESSION_SECRET}"
    volumes:
      - slopr-data:/data
      - ./mcp.json:/config/mcp.json:ro
      # per-user volumes are mounted manually under /data/users/<user-id>/
    depends_on:
      - searxng
      - tika

  searxng:
    image: searxng/searxng:latest
    environment:
      BASE_URL: "http://localhost:8888/"
    ports:
      - "8888:8080"

  tika:
    # -full bundles Tesseract for OCR (pin a concrete version in real deployments)
    image: apache/tika:2.9.2.1-full

volumes:
  slopr-data:
```

- [ ] **Step 4: Create the example MCP config**

Create `mcp.json`:
```json
{
  "servers": {
    "searxng": {
      "transport": "http",
      "url": "http://searxng-mcp:8080/sse"
    },
    "fetch": {
      "transport": "http",
      "url": "http://fetch-mcp:8080/sse"
    }
  }
}
```
> Phase 4 implements the MCP client that reads this file. Per the spec, MCP servers run as their own HTTP/SSE containers; add their service definitions to `compose.yaml` in Phase 4.

- [ ] **Step 5: Create `.env.example`**

Create `.env.example`:
```
SLOPR_SESSION_SECRET=change-me-to-a-long-random-string
SLOPR_ADMIN_INITIAL_PASSWORD=change-me
SLOPR_CHAT_BASE_URL=http://your-mimo-host/v1
SLOPR_CHAT_API_KEY=
SLOPR_CHAT_MODEL=MiMo
SLOPR_EMBED_BASE_URL=https://api.openai.com/v1
SLOPR_EMBED_API_KEY=sk-...
```

- [ ] **Step 6: Validate the compose file**

Run: `docker compose -f compose.yaml config >/dev/null && echo OK` (or `podman-compose config`)
Expected: prints `OK` (config parses).

- [ ] **Step 7: Commit**

```bash
git add backend/Containerfile backend/.dockerignore compose.yaml mcp.json .env.example
git commit -m "chore: add multi-stage Containerfile and compose skeleton (slopr+searxng+tika)"
```

---

## Task 11: End-to-end foundation verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend test suite**

Run: `cd backend && go test ./...`
Expected: all packages PASS, including the sqlite-vec smoke test.

- [ ] **Step 2: Frontend test + build**

Run: `cd frontend && npm run test -- --run && npm run build`
Expected: tests pass; `backend/web/dist` populated.

- [ ] **Step 3: Run the single binary serving the real SPA**

Run:
```bash
cd backend && CGO_ENABLED=0 go build -o /tmp/slopr ./cmd/slopr
SLOPR_SESSION_SECRET=dev SLOPR_ADMIN_INITIAL_PASSWORD=dev SLOPR_DB_PATH=/tmp/slopr.db /tmp/slopr &
sleep 1
curl -s localhost:8080/api/health        # {"status":"ok",...}
curl -s localhost:8080/ | head -c 100     # served index.html from embed
kill %1
```
Expected: health JSON ok; `/` returns the built `index.html`.

- [ ] **Step 4: Container build (if a container runtime is available)**

Run: `docker build -f backend/Containerfile -t slopr:dev . && echo BUILT` (or `podman build`)
Expected: image builds; prints `BUILT`.

- [ ] **Step 5: Final commit / tag the phase**

```bash
git add -A
git commit -m "chore: phase 1 foundation complete" --allow-empty
```

---

## Self-Review

**Spec coverage (Phase 1 items from "Implementation order" §1):**
- Repo scaffold (Go + React/Vite/Tailwind) → Tasks 1, 9. ✓
- Go server serving JSON/SSE API + embedded React via `embed.FS` → Tasks 5, 6, 7, 8. ✓
- SQLite store + `sqlite-vec` verified early → Tasks 3, 4. ✓
- Config (ENV) → Task 2. ✓
- Containerfile/compose skeleton (runtime-agnostic; slopr+searxng+tika; MCP as separate containers) → Task 10. ✓
- UI direction A palette + Anthropic-font wiring (with documented swap point) → Task 9. ✓
- `mcp.json` example present (client deferred to Phase 4, noted) → Task 10. ✓

**Placeholder scan:** No "TBD/TODO"; the one font swap-point is concrete working code (open stand-ins) with an explicit, documented replacement instruction — not a gap. The cgo fallback in Task 4 is a conditional with full instructions, not a placeholder.

**Type/name consistency:** `store.Open(path) (*sql.DB, error)`, `config.Load() (Config, error)`, `httpapi.New(Deps) http.Handler` with `Deps{Version, Static}`, `sse.NewWriter(w) (*Writer, error)` + `(*Writer).Send(event, data)`, `web.SPAHandler() http.Handler` — used consistently across Tasks 5–10. CSS custom props (`--slopr-*`) match between `tokens.css`, `index.css`, and `tailwind.config.ts`.

**Out of scope (correctly deferred):** auth/sessions (Phase 2), projects/threads/messages tables (Phase 3), MCP client (Phase 4), documents/RAG + vec tables (Phase 5), memory (Phase 6). Phase 1 ships only `settings` + `schema_migrations`.
