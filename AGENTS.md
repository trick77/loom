# loom

Self-hosted multi-user LLM chat: Go backend (JSON/SSE API) + embedded React SPA.

## Working conventions
- TDD.
- Commit subjects + PR titles: Go style `area: what` (`turn: …`, `ui: …`), gated by ship.
- Fresh clone: `ship hooks` before the first push (check, conform and init install it too); without the pre-push hook a push skips the gate.
- Gate: `make check` (`ship check`), floors 75 (Go, UI, patch). Raise tests, never a floor. Sandbox escape
  test is CI only (`sandbox-escape.yaml`, docker); isolation change → `make sandbox-escape-test` locally.
- Release by hand: `gh workflow run Release -f bump=auto` (`Release: minor`/`Release-note: …` lines).
- What kit (`github.com/trick77/ship/kit`) provides → from kit, never hand-rolled. Lint config is ship's.

## Locked technical choices (change only with explicit agreement)
- **Pure-Go SQLite**: `ncruces/go-sqlite3` **v0.23.3** + `sqlite-vec-go-bindings/ncruces`
  **v0.1.7-alpha.2**, bumped together by hand (v0.24+ breaks the binding ABI). `CGO_ENABLED=0`; never
  `mattn/go-sqlite3`. One SQLite file, `sqlite-vec` for vectors.
- stdlib `net/http`, no framework. Streaming: SSE (kit `sse`).
- LLMs only via `github.com/trick77/toolkit/llmwire` (wire + every model fact); loom owns routing, budgets,
  prompts, accounting. **Never name a model, vendor or model behaviour in loom**: intents
  (`ReasoningMinimal`/`ReasoningBalanced`, `MaxAnswerTokens`), profile fields; a missing fact goes into
  llmwire. Tests use `llmwiretest`. Extraction: Tika sidecar.
- Tools are MCP-backed; extras in `BACKEND_MCP_SERVERS_FILE` (`/conf/mcp.json`, overrides built-ins by
  name, secrets only `${VAR}`). Outside service behind a tool → own MCP sidecar (`loom-<name>`), contract in
  `docs/<name>.md`, swappable by an `mcp.json` entry of that name. loom keeps policy via `toolSpec` flags,
  never a tool-name check, vendor import or vendor arg. Results only via text, `structuredContent.sources`,
  files, images (`artifacts: true`). `users` = loom user id.
- `run_python` → `loom-sandbox` (`docs/sandbox.md`): a job never gets `/data`, a user id or a socket.

## Config
- `BACKEND_*` env: empty = unset (kit env); `off` disables an optional one.
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
- `docs/schema.md` = `TestSchemaDoc` golden; regen in `backend/` after a migration or model.go change.
  Covers model.go types (name, dir, doc, `Table:`) and fields of `Table: -` types only; Table-bound
  fields and field-vs-column match go unchecked.
- Next free `backend/internal/store/migrations/NNNN_*.sql` (kit runner refuses duplicates); never edit an applied one.
- Rebuild needing FKs off: first line `-- migrate:no-transaction`, then `PRAGMA foreign_keys = OFF; BEGIN;
  … COMMIT; PRAGMA foreign_keys = ON;` (0032). Migrations run on their own pool, a failed one leaks no
  FK-off connection.
- **vec0 never frees a deleted vector**: every KNN reads the partition's whole chunks (rongo: minutes per
  search at 46k vectors in 380 chunks). `CompactVectorsAtBoot` rebuilds past 2x, BOOT ONLY: it holds the
  write lock for minutes (rongo: 8m33s), far past the busy timeout. Deletes only warn.

## Frontend
- Direction A: themed classes from `ui/src/index.css` tokens (`bg-panel`, `text-ink`).

## Queued messages
- Sent during a running turn → `queued_messages`, moved into `messages` only by `ConsumeQueued` (one tx,
  rowid = pickup time). Enqueue, consume, stop+drain (ONE locked step, `stopAndDrain`) and hand-off run
  under the thread's queue lock (`activeStreamRegistry`), never the global lock: every unlocked path lost
  or orphaned messages; stop and drain apart let the canceled turn answer the queue first (CI flake).
  Stop hands queued text back; a stopped stream hands the rest on only after its turn ended.

## Security invariants
- Every DB query scoped by `user_id`. Per-user file access confined to the user's root (`..`, absolute,
  symlink escape rejected). Admin endpoints role-gated.
- Personal data → own token per user, never shared (`PerUser`; `_USERS` only grants).
