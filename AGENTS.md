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
- `make fe-lint` — frontend lint (oxlint: rules-of-hooks, exhaustive-deps, unused vars)
- `make model-names` — fails on a model/vendor name outside `.env.example` (CI runs it)
- `make coverage-gate` — 80% on changed lines + project floors; needs `pip install diff-cover==10.3.0`.
- `make fe-build` — build the SPA into `backend/web/dist` (embedded by Go)
- `make build` — full build → `bin/loom` (CGO_ENABLED=0)
- `make run` — run locally (needs `BACKEND_SESSION_SECRET` + `BACKEND_AUTH_MODE`; `make dev` sets both)
- `docker compose up --build` — full stack (copy `.env.example` → `.env` and fill it first)

## Locked technical choices (do not change without explicit agreement)
- Module path `github.com/trick77/loom`. Go 1.26 (`go.mod`; Containerfile uses `golang:1.27-alpine`).
- **Pure-Go SQLite**: `ncruces/go-sqlite3` pinned to **`v0.23.3`** + `sqlite-vec-go-bindings/ncruces`
  pinned to **`v0.1.7-alpha.2`**.
  `CGO_ENABLED=0` everywhere. Do NOT switch to `mattn/go-sqlite3` — the pin matches the sqlite-vec
  binding's ABI; `ncruces/go-sqlite3` v0.24+ breaks the current sqlite-vec binding.
- One SQLite file; `sqlite-vec` for vectors. No separate DB service.
- HTTP: stdlib `net/http` (Go 1.22 method routing), no web framework. Streaming: **SSE**.
- LLMs only via `github.com/trick77/llmwire`: it owns the wire and every model fact (profiles); loom
  owns routing, budgets, prompts, accounting. **Never name a model, vendor or model behaviour in
  loom**: use intents (`ReasoningMinimal`/`ReasoningBalanced`, `MaxAnswerTokens`) and profile fields;
  a missing fact goes into llmwire. Tests use `llmwiretest`. Extraction: Apache **Tika** sidecar.
- Tools are **MCP-backed**. Tavily via `BACKEND_TAVILY_API_KEY`; `fetch__fetch` runs **in-process**
  (`github.com/trick77/webfetch`); Obscura sidecar via `BACKEND_OBSCURA_MCP_URL`. Best-effort extras
  (Context7, ipverse-lens) live in `BACKEND_MCP_SERVERS_FILE` (`mcpServers` JSON, default
  `/conf/mcp.json`, overrides built-ins by name); secrets only as `${VAR}` interpolation.

## Config
- Runtime config comes from `BACKEND_*` env vars — see `backend/internal/config/config.go` and
  `.env.example`. Required to boot: `BACKEND_SESSION_SECRET` and `BACKEND_AUTH_MODE` (`oidc` with its
  issuer/client settings, or `dev` on loopback).
- Models are config: `BACKEND_CHAT_MODEL` (+ optional `BACKEND_GATE_MODEL`, `BACKEND_VISION_MODEL`),
  `BACKEND_EMBED_MODEL` (width change → vector table rebuilt at boot, background re-embed); ids are
  checked at boot against llmwire's registry. Keys: `LLMWIRE_<PROVIDER>_API_KEY`; compose loads
  `.env` via `env_file`, so a swap is `.env` only.
- Gates (titles, classification, image intent/description) ask `ReasoningMinimal`; turns, forced
  final answer, prose helpers `ReasoningBalanced` (minimal may mean thinking off: wrong on prose).
- Secrets via env only; never commit them. The `admin` account is seeded from env on first boot only.

## Database / migrations
- Add a migration as a new numbered file `backend/internal/store/migrations/NNNN_*.sql`. The runner
  applies pending ones in order and records them in `schema_migrations`.
- Never edit an already-applied migration — add a new one.

## Frontend
- Vite + React + TS + Tailwind, **direction A (Warm Editorial)**: tokens are `--ui-*` CSS variables in
  `ui/src/index.css`; use the themed classes (`bg-bg`, `bg-panel`, `text-ink`, `text-muted`,
  `bg-accent`, `rounded-ui`, `font-serif`/`font-sans`). Anthropic fonts are self-hosted there.
- `npm run build` empties `backend/web/dist` and overwrites the tracked placeholder `index.html`.
  Do NOT commit built assets — only that placeholder is tracked; restore it
  (`git checkout -- backend/web/dist/index.html`) after a local build.

## Security invariants (must hold in every feature)
- Every DB query is scoped by `user_id`; no cross-user access to any resource.
- All per-user volume file access is sandboxed to the user's root: reject `..`, absolute paths, and
  symlink escape.
- Admin-only endpoints are role-gated.
