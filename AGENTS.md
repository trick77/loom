# loom

Self-hosted multi-user LLM chat: Go backend (JSON/SSE API) + embedded React SPA.

## Working conventions
- Docs, specs, code comments English only (maintainer talks German). TDD. YAML is `.yaml`.
- One branch per change, never commit to `master`.
- Commit subjects + PR titles: Go style `area: what` (`httpapi: …`, `chat: …`, `rag: …`, `store: …`, `sandbox: …`, `turn: …`, `ui: …`), gated by ship; release notes list them by area.
- Gate: `make check` (`ship check`): conform, lint, tests, floors 75 (Go, UI), patch 75, `ship.yaml` steps.
  `make hooks` once per clone runs it on every push. Raise tests, never a floor. Sandbox escape test is CI
  only (`sandbox-escape.yaml`, docker); isolation change → `make sandbox-escape-test` locally.
- Release by hand: `gh workflow run Release -f bump=auto` (patch, or `Release: minor` + `Release-note: …`
  lines). Builds, scans and signs both images.
- Plumbing kit (`github.com/trick77/ship/kit`) provides comes from kit, never hand-rolled. Lint config is
  ship's, no local copy.

## Locked technical choices (change only with explicit agreement)
- Go 1.27. **Pure-Go SQLite**: `ncruces/go-sqlite3` **v0.23.3** + `sqlite-vec-go-bindings/ncruces`
  **v0.1.7-alpha.2**, bumped together by hand (v0.24+ breaks the binding ABI). `CGO_ENABLED=0`; never
  `mattn/go-sqlite3`. One SQLite file, `sqlite-vec` for vectors.
- stdlib `net/http`, no framework. Streaming: SSE (kit `sse`).
- LLMs only via `github.com/trick77/llmwire` (wire + every model fact); loom owns routing, budgets,
  prompts, accounting. **Never name a model, vendor or model behaviour in loom**: intents
  (`ReasoningMinimal`/`ReasoningBalanced`, `MaxAnswerTokens`), profile fields; a missing fact goes into
  llmwire. Tests use `llmwiretest`. Extraction: Tika sidecar.
- Tools are MCP-backed. `fetch__fetch` in-process (`webfetch`); Tavily, Obscura built in; extras in
  `BACKEND_MCP_SERVERS_FILE` (default `/conf/mcp.json`, overrides built-ins by name, secrets only as
  `${VAR}`). Sources → `structuredContent.sources`, no tool-name case. `users` = loom user id.
- `run_python` → `loom-sandbox` (`docs/sandbox.md`): a job never gets `/data`, a user id or a socket.

## Config
- `BACKEND_*` env (`config.go`, `.env.example`). Empty = unset (kit env); `off` disables an optional one.
- Public URL only from `PUBLIC_FQDN` (Traefik rule too): `https://<PUBLIC_FQDN>`, required in oidc; dev
  `http://<BACKEND_ADDR>`. Never a separate public-URL var (blank one → cookies without Secure, unnoticed).
- Boot needs `BACKEND_AUTH_MODE` (`oidc` + issuer/client, or `dev` on loopback) and
  `BACKEND_SESSION_SECRET` ≥ 32 chars: it signs the session cookie (kit/auth). No session table: logout only
  clears the cookie, rotating the secret signs everyone out.
- Models: `BACKEND_CHAT_MODEL` (+ `_GATE_`, `_VISION_`), `BACKEND_EMBED_MODEL` (change → vector table
  rebuilt at boot, re-embed in background); checked against llmwire. Keys `LLMWIRE_<PROVIDER>_API_KEY`.
- Gates (titles, classification, image intent) ask `ReasoningMinimal`; turns and prose `ReasoningBalanced`
  (minimal may mean thinking off: wrong on prose).
- Secrets via env only. `admin` is seeded from env on first boot only.

## Database / migrations
- Next free number `store/migrations/NNNN_*.sql`, never a taken one (kit runner refuses duplicates).
  Never edit an applied migration.
- Rebuild needing FKs off: first line `-- migrate:no-transaction`, then `PRAGMA foreign_keys = OFF; BEGIN;
  … COMMIT; PRAGMA foreign_keys = ON;` (0032). Migrations run on their own pool, a failed one leaks no
  FK-off connection.
- **vec0 never frees a deleted vector**: every KNN reads the partition's whole chunks (rongo: minutes per
  search at 46k vectors in 380 chunks). `CompactVectorsAtBoot` rebuilds past 2x, BOOT ONLY: it holds the
  write lock for minutes (rongo: 8m33s), far past the busy timeout. Deletes only warn.

## Frontend
- Vite + React + TS + Tailwind, direction A (Warm Editorial): `--ui-*` tokens in `ui/src/index.css`,
  themed classes (`bg-bg`, `bg-panel`, `text-ink`, `text-muted`, `bg-accent`, `rounded-ui`).
- Build writes `backend/web/dist` (the Go embed, only `.gitkeep` tracked); unbuilt binary serves
  `web/placeholder.html` with 503. Never commit built assets.

## Queued messages
- Sent during a running turn → `queued_messages`, moved into `messages` only by `ConsumeQueued` (one tx,
  rowid = pickup time). Enqueue, consume, stop+drain (ONE locked step, `stopAndDrain`) and hand-off run
  under the thread's queue lock (`activeStreamRegistry`), never the global lock: every unlocked path lost
  or orphaned messages; stop and drain apart let the canceled turn answer the queue first (CI flake).
  Stop hands queued text back; a stopped stream hands the rest on only after its turn ended.

## Security invariants
- Every DB query scoped by `user_id`. Per-user file access confined to the user's root (`..`, absolute,
  symlink escape rejected). Admin endpoints role-gated.
