# loom

Self-hosted, multi-user LLM chat app: Go backend serving a JSON/SSE API + an embedded React SPA.

## Working conventions
- Docs, specs, and code comments are **English only** (conversation with the maintainer is German).
- One feature branch per phase (`feat/phase-N-...`); never commit to `master`. Conventional commits.
- TDD: write the failing test first, then the minimal implementation.
- Keep files focused — one clear responsibility each.
- YAML files use the `.yaml` extension (never `.yml`).

## Commands
- `make test` — backend Go tests (`go test ./...`)
- `make fe-test` — frontend Vitest
- `make coverage-gate` — coverage gates: 80% on lines the branch changes, plus project
  floors. Needs `pip install diff-cover==10.3.0`. New code must be tested; legacy debt is ignored.
- `make fe-build` — build the SPA into `backend/web/dist` (embedded by Go)
- `make build` — full build → `bin/loom` (CGO_ENABLED=0)
- `make run` — run locally (needs `BACKEND_SESSION_SECRET` + `BACKEND_ADMIN_INITIAL_PASSWORD`)
- `docker compose up --build` — full stack (copy `.env.example` → `.env` and fill it first)

## Locked technical choices (do not change without explicit agreement)
- Module path `github.com/trick77/loom`. Go 1.25 (`go.mod`; Containerfile uses `golang:1.25-alpine`).
- **Pure-Go SQLite**: `ncruces/go-sqlite3` pinned to **`v0.23.3`** + `sqlite-vec-go-bindings/ncruces`
  pinned to **`v0.1.7-alpha.2`**.
  `CGO_ENABLED=0` everywhere. Do NOT switch to `mattn/go-sqlite3` — the pin matches the sqlite-vec
  binding's ABI; `ncruces/go-sqlite3` v0.24+ breaks the current sqlite-vec binding.
- One SQLite file; `sqlite-vec` for vectors. No separate DB service.
- HTTP: stdlib `net/http` (Go 1.22 method routing), no web framework. Streaming: **SSE**.
- One OpenAI-compatible client for chat (MiMo) + embeddings (OpenAI). Extraction: Apache **Tika** sidecar.
- Tools/agents are **first-class MCP-backed integrations**. Tavily web search is enabled with
  `BACKEND_TAVILY_API_KEY`; the `fetch__fetch` page reader runs **in-process** (shared
  `github.com/trick77/webfetch` module, no sidecar); the Obscura browser sidecar uses
  `BACKEND_OBSCURA_MCP_URL`. Secondary, best-effort servers (Context7 docs, ipverse-lens whois) are
  declared in a JSON file (standard `mcpServers` format) at `BACKEND_MCP_SERVERS_FILE` (default
  `/conf/mcp.json`); its entries merge on top of — and override, by name — the built-ins. Keep secrets
  out of the file: use `${VAR}` interpolation so tokens stay in env (e.g. `CONTEXT7_API_KEY`,
  `IPVERSE_API_KEY`).

## Config
- All runtime config comes from `BACKEND_*` env vars — see `backend/internal/config/config.go` and
  `.env.example`. Required to boot: `BACKEND_SESSION_SECRET`, `BACKEND_ADMIN_INITIAL_PASSWORD`.
- Secrets via env only; never commit them. The `admin` account is seeded from env on first boot only.

## Database / migrations
- Add a migration as a new numbered file `backend/internal/store/migrations/NNNN_*.sql`. The runner
  applies pending ones in order and records them in `schema_migrations`.
- Never edit an already-applied migration — add a new one.

## Frontend
- Vite + React + TS + Tailwind. UI is **direction A (Warm Editorial)**: design tokens are CSS variables
  `--ui-*` in `ui/src/index.css`; use the themed Tailwind classes (`bg-bg`, `bg-panel`,
  `text-ink`, `text-muted`, `bg-accent`, `rounded-ui`, `font-serif`/`font-sans`). The Anthropic
  variable fonts (`Anthropic Sans`/`Serif`/`Mono`/`Icons`) are self-hosted via `@font-face` in
  `ui/src/index.css` and wired to `--font-sans`/`--font-serif`/`--font-mono`.
- `npm run build` empties `backend/web/dist` and overwrites the tracked placeholder `index.html`.
  Do NOT commit built assets — only that placeholder is tracked; restore it
  (`git checkout -- backend/web/dist/index.html`) after a local build.

## Security invariants (must hold in every feature)
- Every DB query is scoped by `user_id`; no cross-user access to any resource.
- All per-user volume file access is sandboxed to the user's root: reject `..`, absolute paths, and
  symlink escape.
- Admin-only endpoints are role-gated.
