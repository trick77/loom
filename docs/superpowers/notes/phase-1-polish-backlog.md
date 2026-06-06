# Phase 1 — Polish Backlog (deferred minors)

Accepted, non-blocking items from the Phase 1 reviews. Address in the polish phase (Phase 7) or
opportunistically when touching the relevant code.

## Backend
- **recovery middleware** (`internal/httpapi/middleware.go`): re-panic on `http.ErrAbortHandler`
  instead of swallowing it into a 500.
- **logging middleware**: capture and log the HTTP response status code (wrap ResponseWriter).
- **store** (`internal/store`): consider `db.SetMaxOpenConns(1)` for SQLite write serialization;
  capture `tx.Rollback()` return (or `_ =` it) to satisfy linters; add a migration-error rollback test.
- **health stream**: add a route-level test for `GET /api/health/stream` through `httpapi.New()`
  (currently only `sse.Writer` is unit-tested).
- Unknown `/api/*` paths currently fall through to the SPA catch-all (200 index.html) instead of 404 —
  revisit once a real API surface exists.

## Frontend
- **emptyOutDir churn**: `vite build` deletes the tracked `backend/web/dist/.gitkeep` and overwrites the
  placeholder `index.html`, dirtying the working tree. Decide a clean strategy (e.g. gitignore the whole
  embed dir + generate a stub at build/CI time, or a Makefile restore step).
- Remove the redundant `@testing-library/jest-dom/vitest` import in `src/App.test.tsx:1`
  (already provided by `vitest.setup.ts`).
- `active` color/token is defined but unused until chat list items exist (Phase 3).
